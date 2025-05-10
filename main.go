package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	vallox "github.com/pvainio/vallox-rs485"

	"github.com/kelseyhightower/envconfig"

	mqttClient "github.com/eclipse/paho.mqtt.golang"
)

type cacheEntry struct {
	time  time.Time
	value vallox.Event
}

const (
	topicFanSpeed            = "vallox/fan/speed"
	topicFanSpeedSet         = "vallox/fan/set"
	topicTempIncomingInside  = "vallox/temp/incoming/inside"
	topicTempIncomingOutside = "vallox/temp/incoming/outside"
	topicTempOutgoingInside  = "vallox/temp/outgoing/inside"
	topicTempOutgoingOutside = "vallox/temp/outgoing/outside"
	topicTempHexBypass       = "vallox/temp/hexbypass"
	topicTempPostHeating	 = "vallox/temp/postheating"
	topicLights              = "vallox/lights"
	topicErrorCode           = "vallox/errorcode"
	topicTimeBoosting        = "vallox/time/boosting"
	topicIOPort              = "vallox/misc/ioport"
	topicFlags2              = "vallox/misc/flags2"
	topicFlags6              = "vallox/misc/flags6"
)

var topicMap = map[byte]string{
	vallox.FanSpeed:            topicFanSpeed,
	vallox.TempIncomingInside:  topicTempIncomingInside,
	vallox.TempIncomingOutside: topicTempIncomingOutside,
	vallox.TempOutgoingInside:  topicTempOutgoingInside,
	vallox.TempOutgoingOutside: topicTempOutgoingOutside,
	vallox.TempHexBypass:       topicTempHexBypass,
	vallox.TempPostHeating:     topicTempPostHeating,
	vallox.Lights:              topicLights,
	vallox.ErrorCode:           topicErrorCode,
        vallox.TimeBoosting:        topicTimeBoosting,
        vallox.IOPort:              topicIOPort,
        vallox.Flags2:              topicFlags2,
        vallox.Flags6:              topicFlags6,
}

type Config struct {
	SerialDevice string `envconfig:"serial_device" required:"true"`
	MqttUrl      string `envconfig:"mqtt_url" required:"true"`
	MqttUser     string `envconfig:"mqtt_user"`
	MqttPwd      string `envconfig:"mqtt_password"`
	MqttClientId string `envconfig:"mqtt_client_id" default:"vallox"`
	Debug        bool   `envconfig:"debug" default:"false"`
	EnableWrite  bool   `envconfig:"enable_write" default:"false"`
	SpeedMin     byte   `envconfig:"speed_min" default:"1"`
	EnableRaw    bool   `envconfig:"enable_raw" default:"false"`
	Monitor      bool   `envconfig:"enable_monitor" default:"false"`
}

var (
	config Config

	logDebug *log.Logger
	logInfo  *log.Logger
	logError *log.Logger

	updateSpeed          byte
	updateSpeedRequested time.Time
	currentSpeed         byte
	currentSpeedUpdated  time.Time

	speedUpdateRequest = make(chan byte, 10)
	speedUpdateSend    = make(chan byte, 10)

	homeassistantStatus = make(chan string, 10)
)

func init() {

	err := envconfig.Process("vallox", &config)
	if err != nil {
		log.Fatal(err.Error())
	}

	initLogging()
}

func main() {

	mqtt := connectMqtt()

	valloxDevice := connectVallox()

	cache := make(map[byte]cacheEntry)

	announceMeToMqttDiscovery(mqtt, cache)

	for {
		select {
		case event := <-valloxDevice.Events():
			handleValloxEvent(valloxDevice, event, cache, mqtt)
		case request := <-speedUpdateRequest:
			if hasSameRecentSpeed(request) {
				continue
			}
			updateSpeed = request
			updateSpeedRequested = time.Now()
			speedUpdateSend <- request
		case <-speedUpdateSend:
			sendSpeed(valloxDevice)
		case status := <-homeassistantStatus:
			if status == "online" {
				// HA became online, send discovery so it knows about entities
				go announceMeToMqttDiscovery(mqtt, cache)
			} else if status != "offline" {
				logInfo.Printf("unknown HA status message %s", status)
			}
		}
	}
}

func handleValloxEvent(valloxDev *vallox.Vallox, e vallox.Event, cache map[byte]cacheEntry, mqtt mqttClient.Client) {
	//logDebug.Printf("Event from 0x%x to 0x%x: (0x%x, 0x%x)\n", e.Source, e.Destination, e.Register, e.RawValue);

	if !config.Monitor && !valloxDev.ForMe(e) {
		return // Ignore values not addressed for me
	}

	if val, ok := cache[e.Register]; !ok {
		// First time we receive this value, send Home Assistant discovery
		announceRawData(mqtt, e.Register)
	} else if val.value.RawValue == e.RawValue && time.Since(val.time) < time.Duration(15)*time.Minute {
		// Some values are not published by the device, so manually republish to keep the device online
		resendOldValues(valloxDev, mqtt, cache)
		// we already have that value and have recently published it, no need to publish to mqtt
		return
	}

	cached := cacheEntry{time: time.Now(), value: e}
	cache[e.Register] = cached

	if e.Register == vallox.FanSpeed {
		currentSpeed = byte(e.Value)
		currentSpeedUpdated = cached.time
	}

	go publishValue(mqtt, cached.value)
}

func sendSpeed(valloxDevice *vallox.Vallox) {
	if time.Since(updateSpeedRequested) < time.Duration(5)*time.Second {
		// Less than second old, retry later
		go func() {
			time.Sleep(time.Duration(1000) * time.Millisecond)
			speedUpdateSend <- updateSpeed
		}()
	} else if currentSpeed != updateSpeed || time.Since(currentSpeedUpdated) > 10*time.Second {
		logDebug.Printf("sending speed update to %x", updateSpeed)
		currentSpeed = updateSpeed
		currentSpeedUpdated = time.Now()
		valloxDevice.SetSpeed(updateSpeed)
		time.Sleep(time.Duration(20) * time.Millisecond)
		valloxDevice.Query(vallox.FanSpeed)
	}
}

func hasSameRecentSpeed(request byte) bool {
	return currentSpeed == request && time.Since(currentSpeedUpdated) < time.Duration(10)*time.Second
}

func connectVallox() *vallox.Vallox {
	cfg := vallox.Config{Device: config.SerialDevice, EnableWrite: config.EnableWrite, LogDebug: logDebug}

	logInfo.Printf("connecting to vallox serial port %s write enabled: %v", cfg.Device, cfg.EnableWrite)

	valloxDevice, err := vallox.Open(cfg)

	if err != nil {
		logError.Fatalf("error opening Vallox device %s: %v", config.SerialDevice, err)
	}

	return valloxDevice
}

func connectMqtt() mqttClient.Client {

	opts := mqttClient.NewClientOptions().
		AddBroker(config.MqttUrl).
		SetClientID(config.MqttClientId).
		SetOrderMatters(false).
		SetKeepAlive(150 * time.Second).
		SetAutoReconnect(true).
		SetConnectionLostHandler(connectionLostHandler).
		SetOnConnectHandler(connectHandler).
		SetReconnectingHandler(reconnectHandler)

	if len(config.MqttUser) > 0 {
		opts = opts.SetUsername(config.MqttUser)
	}

	if len(config.MqttPwd) > 0 {
		opts = opts.SetPassword(config.MqttPwd)
	}

	logInfo.Printf("connecting to mqtt %s client id %s user %s", opts.Servers, opts.ClientID, opts.Username)

	c := mqttClient.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	return c
}

func changeSpeedMessage(mqtt mqttClient.Client, msg mqttClient.Message) {
	body := string(msg.Payload())
	topic := msg.Topic()
	logInfo.Printf("received speed change %s to %s", body, topic)
	spd, err := strconv.ParseInt(body, 0, 8)
	if err != nil {
		logError.Printf("cannot parse speed from body %s", body)
	} else {
		speedUpdateRequest <- byte(spd)
	}
}

func haStatusMessage(mqtt mqttClient.Client, msg mqttClient.Message) {
	body := string(msg.Payload())
	homeassistantStatus <- body
}

func subscribe(mqtt mqttClient.Client) {
	logDebug.Print("subscribing to topics")
	mqtt.Subscribe("homeassistant/status", 0, haStatusMessage)
	mqtt.Subscribe("vallox/fan/set", 0, changeSpeedMessage)
}

func resendOldValues(device *vallox.Vallox, mqtt mqttClient.Client, cache map[byte]cacheEntry) {
	// Speed is not automatically published by Vallox, so manually refresh the value
	now := time.Now()
	validTime := now.Add(time.Duration(-15) * time.Minute)
	if cached, ok := cache[vallox.FanSpeed]; ok && cached.time.Before(validTime) || !ok {
		device.Query(vallox.FanSpeed)
	}
}

func publishValue(mqtt mqttClient.Client, event vallox.Event) {

	if topic, ok := topicMap[event.Register]; ok {
		publish(mqtt, topic, fmt.Sprintf("%d", event.Value))
	}

	if config.EnableRaw {
		publish(mqtt, fmt.Sprintf("vallox/raw/%x", event.Register), fmt.Sprintf("%d", event.RawValue))
	}
}

func publish(mqtt mqttClient.Client, topic string, msg interface{}) {
	logDebug.Printf("publishing to %s msg %s", msg, topic)

	t := mqtt.Publish(topic, 0, false, msg)
	go func() {
		_ = t.Wait()
		if t.Error() != nil {
			logError.Printf("publishing msg failed %v", t.Error())
		}
	}()
}

func discoveryMsg(uid string, name string, stateTopic string, commandTopic string) []byte {
	msg := make(map[string]interface{})
	msg["unique_id"] = uid
	msg["name"] = name

	dev := make(map[string]string)
	msg["device"] = dev
	dev["identifiers"] = "vallox"
	dev["manufacturer"] = "Vallox"
	dev["name"] = "Vallox Digit SE"
	dev["model"] = "Digit SE"

	if stateTopic != "" {
		msg["state_topic"] = stateTopic
	}
	if commandTopic != "" {
		msg["command_topic"] = commandTopic
	}

	if uid == "vallox_fan_select" {
		min := int(config.SpeedMin)
		var options []string
		for i := min; i <= 8; i++ {
			options = append(options, strconv.FormatInt(int64(i), 10))
		}
		msg["options"] = options
		msg["icon"] = "mdi:fan"
	} else if uid == "vallox_fan_speed" {
		msg["expire_after"] = 1800
		msg["icon"] = "mdi:fan"
		msg["state_class"] = "measurement"
	}

	if strings.HasPrefix(uid, "vallox_temp") {
		msg["unit_of_measurement"] = "°C"
		msg["state_class"] = "measurement"
		msg["expire_after"] = 1800
		msg["device_class"] = "temperature"
	}

	jsonm, err := json.Marshal(msg)
	if err != nil {
		logError.Printf("cannot marshal json %v", err)
	}
	return jsonm
}

func announceMeToMqttDiscovery(mqtt mqttClient.Client, cache map[byte]cacheEntry) {
	publishDiscovery(mqtt, "vallox_fan_speed", "Vallox speed", topicFanSpeed)
	publishDiscoveryFanSelect(mqtt, "vallox_fan_select", "Vallox speed select", topicFanSpeed)
	publishDiscovery(mqtt, "vallox_temp_incoming_outside", "Vallox outdoor temperature", topicTempIncomingOutside)
	publishDiscovery(mqtt, "vallox_temp_incoming_insise", "Vallox incoming temperature", topicTempIncomingInside)
	publishDiscovery(mqtt, "vallox_temp_outgoing_inside", "Vallox interior temperature", topicTempOutgoingInside)
	publishDiscovery(mqtt, "vallox_temp_outgoing_outside", "Vallox exhaust temperature", topicTempOutgoingOutside)
	publishDiscovery(mqtt, "vallox_temp_hexbypass", "Vallox heat exchanger bypass temperature", topicTempHexBypass)
	publishDiscovery(mqtt, "vallox_temp_postheating", "Vallox post heating target temperature", topicTempPostHeating)
	publishDiscovery(mqtt, "vallox_lights", "Vallox indicator lights", topicLights)
	publishDiscovery(mqtt, "vallox_errorcode", "Vallox latest error code", topicErrorCode)
	publishDiscovery(mqtt, "vallox_time_boosting", "Vallox boosting time left", topicTimeBoosting)
	publishDiscovery(mqtt, "vallox_misc_ioport", "Vallox IO port status", topicIOPort)
	publishDiscovery(mqtt, "vallox_misc_flags2", "Vallox Flags 2", topicFlags2)
	publishDiscovery(mqtt, "vallox_misc_flags6", "Vallox Flags 6", topicFlags6)

	for reg := range cache {
		announceRawData(mqtt, reg)
	}
}

func announceRawData(mqtt mqttClient.Client, register byte) {
	if !config.EnableRaw {
		return
	}
	uid := fmt.Sprintf("vallox_raw_%x", register)
	name := fmt.Sprintf("Vallox raw %x", register)
	stateTopic := fmt.Sprintf("vallox/raw/%x", register)
	publishDiscovery(mqtt, uid, name, stateTopic)
}

func publishDiscovery(mqtt mqttClient.Client, uid string, name string, stateTopic string) {
	discoveryTopic := fmt.Sprintf("homeassistant/sensor/%s/config", uid)
	msg := discoveryMsg(uid, name, stateTopic, "")
	publish(mqtt, discoveryTopic, msg)
}

func publishDiscoveryFanSelect(mqtt mqttClient.Client, uid string, name string, stateTopic string) {
	discoveryTopic := fmt.Sprintf("homeassistant/select/%s/config", uid)
	msg := discoveryMsg(uid, name, stateTopic, topicFanSpeedSet)
	publish(mqtt, discoveryTopic, msg)
}

func connectionLostHandler(client mqttClient.Client, err error) {
	options := client.OptionsReader()
	logError.Printf("MQTT connection to %s lost %v", options.Servers(), err)
}

func connectHandler(client mqttClient.Client) {
	options := client.OptionsReader()
	logInfo.Printf("MQTT connected to %s", options.Servers())
	subscribe(client)
}

func reconnectHandler(client mqttClient.Client, options *mqttClient.ClientOptions) {
	logInfo.Printf("MQTT reconnecting to %s", options.Servers)
}

func initLogging() {
	writer := os.Stdout
	err := os.Stderr

	if config.Debug {
		logDebug = log.New(writer, "DEBUG ", log.Ldate|log.Ltime|log.Lmsgprefix)
	} else {
		logDebug = log.New(ioutil.Discard, "DEBUG ", 0)
	}
	logInfo = log.New(writer, "INFO  ", log.Ldate|log.Ltime|log.Lmsgprefix)
	logError = log.New(err, "ERROR ", log.Ldate|log.Ltime|log.Lmsgprefix)
}
