package main

import (
	"encoding/json"
	"fmt"
	"io"
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
	topicFanSpeed            = "fan/speed"
	topicFanSpeedSet         = "fan/set"
	topicTempIncomingInside  = "temp/incoming/inside"
	topicTempIncomingOutside = "temp/incoming/outside"
	topicTempOutgoingInside  = "temp/outgoing/inside"
	topicTempOutgoingOutside = "temp/outgoing/outside"
	topicTempTargetInside    = "temp/insidetarget"
	topicTempHexBypass       = "temp/hexbypass"
	topicTempPostHeating	 = "temp/postheating"
	topicRhHighest           = "rh/highest"
	topicRh1                 = "rh/sensor1"
	topicRh2                 = "rh/sensor2"
	topicCo2Highest          = "co2/highest"
	topicLights              = "lights"
	topicErrorCode           = "errorcode"
	topicTimeBoosting        = "time/boosting"
	topicIOPort              = "misc/ioport"
	topicFlags2              = "misc/flags2"
	topicFlags6              = "misc/flags6"
	topicRaw                 = "raw/%x"
)

var topicMapOld = map[byte]string{
	vallox.FanSpeed:            topicFanSpeed,
	vallox.TempIncomingInside:  topicTempIncomingInside,
	vallox.TempIncomingOutside: topicTempIncomingOutside,
	vallox.TempOutgoingInside:  topicTempOutgoingInside,
	vallox.TempOutgoingOutside: topicTempOutgoingOutside,
	// vallox.RhHighest:           topicRhHighest,
	// vallox.Rh1:                 topicRh1,
	// vallox.Rh2:                 topicRh2,
	// vallox.Co2HighestHighByte:  topicCo2Highest,
	// vallox.Co2HighestLowByte:   topicCo2Highest,
}

// newer protocol?
var topicMapNew = map[byte]string{
	vallox.FanSpeed:               topicFanSpeed,
	vallox.TempIncomingInsideNew:  topicTempIncomingInside,
	vallox.TempIncomingOutsideNew: topicTempIncomingOutside,
	vallox.TempOutgoingInsideNew:  topicTempOutgoingInside,
	vallox.TempOutgoingOutsideNew: topicTempOutgoingOutside,
	// vallox.RhHighest:              topicRhHighest,
	// vallox.Rh1:                    topicRh1,
	// vallox.Rh2:                    topicRh2,
	// vallox.Co2HighestHighByte:     topicCo2Highest,
	// vallox.Co2HighestLowByte:      topicCo2Highest,
	vallox.TempHexBypass:       topicTempHexBypass,
	vallox.TempPostHeating:     topicTempPostHeating,
	vallox.TempTargetInside:    topicTempTargetInside,
	vallox.Lights:              topicLights,
	vallox.ErrorCode:           topicErrorCode,
        vallox.TimeBoosting:        topicTimeBoosting,
        vallox.IOPort:              topicIOPort,
        vallox.Flags2:              topicFlags2,
        vallox.Flags6:              topicFlags6,
}

var topicMap map[byte]string

var announced map[string]any

type Config struct {
	SerialDevice string `envconfig:"serial_device" required:"true"`
	MqttUrl      string `envconfig:"mqtt_url" required:"true"`
	MqttUser     string `envconfig:"mqtt_user"`
	MqttPwd      string `envconfig:"mqtt_password"`
	MqttClientId string `envconfig:"mqtt_client_id"`
	DeviceId     string `envconfig:"device_id" default:"vallox"`
	DeviceName   string `envconfig:"device_name" default:"Vallox"`
	Debug        bool   `envconfig:"debug" default:"false"`
	EnableWrite  bool   `envconfig:"enable_write" default:"false"`
	SpeedMin     byte   `envconfig:"speed_min" default:"1"`
	EnableRaw    bool   `envconfig:"enable_raw" default:"false"`
	Monitor      bool   `envconfig:"enable_monitor" default:"false"`
	ObjectId     bool   `envconfig:"object_id" default:"true"`
	NewProtocol  bool   `envconfig:"new_protocol" default:"true"`
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

	if config.NewProtocol {
		topicMap = topicMapNew
	} else {
		topicMap = topicMapOld
	}

	if config.MqttClientId == "" {
		config.MqttClientId = config.DeviceId
	}

	initLogging()

	logInfo.Printf("starting with device id %s name %s port %s", config.DeviceId, config.DeviceName, config.SerialDevice)
}

func main() {

	mqtt := connectMqtt()

	valloxDevice := connectVallox()

	cache := make(map[byte]cacheEntry)

	announceMeToMqttDiscovery(mqtt, cache)

	init := time.After(5 * time.Second)
	schedule := time.Tick(5 * time.Minute)

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
		case <-schedule:
			queryValues(valloxDevice, cache)
		case <-init:
			// query initial values
			queryValues(valloxDevice, cache)
		}
	}
}

func handleValloxEvent(valloxDev *vallox.Vallox, e vallox.Event, cache map[byte]cacheEntry, mqtt mqttClient.Client) {
	//logDebug.Printf("Event from 0x%x to 0x%x: (0x%x, 0x%x)\n", e.Source, e.Destination, e.Register, e.RawValue);

	if !config.Monitor && !valloxDev.ForMe(e) {
		return // Ignore values not addressed for me
	}

	logDebug.Printf("received register %x value %x matching %s", e.Register, e.Value, topicMap[e.Register])

	cached, hit := cache[e.Register]
	if !hit {
		// First time we receive this value, send Home Assistant discovery
		announceRawData(mqtt, e.Register)
	}

	if !hit || cached.value.RawValue != e.RawValue || time.Since(cached.time) > time.Duration(1)*time.Minute {
		go publishValue(mqtt, e)
		cached = cacheEntry{time: time.Now(), value: e}
		cache[e.Register] = cached
		if e.Register == vallox.FanSpeed {
			currentSpeed = byte(e.Value)
			currentSpeedUpdated = cached.time
		}
	}
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
	mqtt.Subscribe(topic(topicFanSpeedSet), 0, changeSpeedMessage)
}

func queryValues(device *vallox.Vallox, cache map[byte]cacheEntry) {
	// Speed is not automatically published by Vallox, so manually refresh the value
	logDebug.Printf("scheduled register query")
	now := time.Now()
	validTime := now.Add(time.Duration(-15) * time.Minute)
	for register, _ := range topicMap {
		if cached, ok := cache[register]; !ok || cached.time.Before(validTime) {
			// more than 15min old, query it
			device.Query(register)
		}
	}
}

func publishValue(mqtt mqttClient.Client, event vallox.Event) {

	if t, ok := topicMap[event.Register]; ok {
		publish(mqtt, topic(t), fmt.Sprintf("%d", event.Value))
	}

	if config.EnableRaw {
		publish(mqtt, topic(fmt.Sprintf(topicRaw, event.Register)), fmt.Sprintf("%d", event.RawValue))
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
	msg["unique_id"] = toUid(uid)
	msg["name"] = name
	if config.ObjectId {
		msg["object_id"] = toUid(uid)
	}

	dev := make(map[string]string)
	msg["device"] = dev
	dev["identifiers"] = config.DeviceId
	dev["manufacturer"] = "Vallox"
	dev["name"] = config.DeviceName
	dev["model"] = "Digit SE"

	if stateTopic != "" {
		msg["state_topic"] = topic(stateTopic)
	}
	if commandTopic != "" {
		msg["command_topic"] = topic(commandTopic)
	}

	if uid == "fan_select" {
		min := int(config.SpeedMin)
		var options []string
		for i := min; i <= 8; i++ {
			options = append(options, strconv.FormatInt(int64(i), 10))
		}
		msg["options"] = options
		msg["icon"] = "mdi:fan"
	} else if uid == "fan_speed" {
		msg["expire_after"] = 1800
		msg["icon"] = "mdi:fan"
		msg["state_class"] = "measurement"
	} else if strings.HasPrefix(uid, "temp_") {
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
	announced = make(map[string]any)

	publishSensor(mqtt, "fan_speed", "fan speed", topicFanSpeed)
	publishSelect(mqtt, "fan_select", "fan speed select", topicFanSpeed, topicFanSpeedSet)
	publishSensor(mqtt, "temp_incoming_outside", "outdoor temperature", topicTempIncomingOutside)
	publishSensor(mqtt, "temp_incoming_insise", "incoming temperature", topicTempIncomingInside)
	publishSensor(mqtt, "temp_outgoing_inside", "interior temperature", topicTempOutgoingInside)
	publishSensor(mqtt, "temp_outgoing_outside", "exhaust temperature", topicTempOutgoingOutside)
	publishSensor(mqtt, "temp_target_inside", "inside target temperature", topicTempHexBypass)
	publishSensor(mqtt, "temp_hexbypass", "heat exchanger bypass temperature", topicTempHexBypass)
	publishSensor(mqtt, "temp_postheating", "post heating temperature setpoint", topicTempPostHeating)
	publishSensor(mqtt, "lights", "indicator lights", topicLights)
	publishSensor(mqtt, "errorcode", "latest error code", topicErrorCode)
	publishSensor(mqtt, "time_boosting", "boosting time left", topicTimeBoosting)
	publishSensor(mqtt, "misc_ioport", "IO port status", topicIOPort)
	publishSensor(mqtt, "misc_flags2", "Flags 2", topicFlags2)
	publishSensor(mqtt, "misc_flags6", "Flags 6", topicFlags6)

	for reg := range cache {
		announceRawData(mqtt, reg)
	}
}

func announceRawData(mqtt mqttClient.Client, register byte) {
	if !config.EnableRaw {
		return
	}
	uid := fmt.Sprintf("raw_%x", register)
	name := fmt.Sprintf("raw %x", register)
	stateTopic := fmt.Sprintf(topicRaw, register)
	publishSensor(mqtt, uid, name, stateTopic)
}

func publishSensor(mqtt mqttClient.Client, uid string, name string, stateTopic string) {
	publishDiscovery(mqtt, "sensor", uid, name, stateTopic, "")
}

func publishSelect(mqtt mqttClient.Client, uid string, name string, stateTopic string, cmdTopic string) {
	publishDiscovery(mqtt, "select", uid, name, stateTopic, cmdTopic)
}

func publishDiscovery(mqtt mqttClient.Client, etype string, uid string, name string, stateTopic string, cmdTopic string) {
	discoveryTopic := fmt.Sprintf("homeassistant/%s/%s/config", etype, toUid(uid))
	if _, ok := announced[discoveryTopic]; ok {
		// already announced
		return
	}
	announced[discoveryTopic] = true
	msg := discoveryMsg(uid, name, stateTopic, cmdTopic)
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
		logDebug = log.New(io.Discard, "DEBUG ", 0)
	}
	logInfo = log.New(writer, "INFO  ", log.Ldate|log.Ltime|log.Lmsgprefix)
	logError = log.New(err, "ERROR ", log.Ldate|log.Ltime|log.Lmsgprefix)
}

func toUid(uid string) string {
	return config.DeviceId + "_" + uid
}

func topic(topic string) string {
	return config.DeviceId + "/" + topic
}
