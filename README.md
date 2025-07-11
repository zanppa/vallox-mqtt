# Vallox RS485 MQTT gateway for Home Assistant

## Overview

This rs485 mqtt gateway can be used to publish events from Vallox rs485 serial bus to mqtt and send commands to Vallox devices via mqtt.

It supports Home Assistant MQTT Discovery but can also be used without Home Assistant.

Only requirement is MQTT Broker to connect to.

## Supported features

Supports following features:
- Home Assistant MQTT discovery, published device automatically to Home Assistant
- Published regular intervals:
  * Ventilation fan speed
  * Outside temperature (sensor.temperature_incoming_outside)
  * Incoming temperature (sensor.temperature_incoming_inside)
  * Inside temperature (sensor.temperature_outgoing_inside)
  * Exhaust temperature (sensor.temperature_outgoing_outside)
- Change ventilation speed

## Supported devices

Use at your own risk.

Only tested with:
- Vallox Digit SE model 3500 SE made in 2001 (one with old led panel, no lcd panel)

Newer devices might use different registers for temperatures, try configuration NEW_PROTOCOL=true for those.

Might work with other Vallox devices with rs485 bus.  There probably are some differences between different devices.  If there are those probably are easy to adapt to.

The application itself has been tested running on Raspberry Pi 3, but probably works just fine with Raspberry Zero or anything running linux.

To compile for Raspberry PI: env GOOS=linux GOARCH=arm go build -o vallox_mqtt

Quality RS485 adapter should be used, there can be strange problems with low quality ones.

## Example usecase

Can be used to monitor and command Vallox ventilation device with Home Assistant.  Raspberry Pi with properer usb to rs485 adapter can act as a gateway between Vallox and MQTT (and Home Assistant).  Automation can be built to increase the speed in case of high CO2 or high humidity even if the Vallox device is not installed with co2 and humidity sensors.

### Home Assistant Card screenshots

Speed select and graph:
![outdoor temp grap](https://github.com/pvainio/vallox-mqtt/blob/main/img/ha-graph-speed.png?raw=true)

Temperature graph:
![outdoor temp grap](https://github.com/pvainio/vallox-mqtt/blob/main/img/ha-graph-temp.png?raw=true)

Outdoor temperature graph:
![outdoor temp grap](https://github.com/pvainio/vallox-mqtt/blob/main/img/ha-graph-outtemp.png?raw=true)

## Configuration

Application is configure with environment variables

| variable        | required | default | description |
|-----------------|:--------:|---------|-------------|
| SERIAL_DEVICE   |    x     |         | serial device, for example /dev/ttyUSB0 |
| MQTT_URL        |    x     |         | mqtt url, for example tcp://10.1.2.3:8883 |
| MQTT_USER       |          |         | mqtt username |
| MQTT_PASSWORD   |          |         | mqtt password |
| MQTT_CLIENT_ID  |          | same as DEVICE_ID  | mqtt client id |
| DEVICE_ID       |          | vallox  | id for homeassistant device and also act as mqtt base topic |
| DEVICE_NAME     |          | Vallox  | Home assistant device name |
| DEBUG           |          | false   | enable debug output, true/false |
| ENABLE_WRITE    |          | false   | enable sending commands/writing to bus, true/false |
| SPEED_MIN       |          | 1       | minimum speed for the device, between 1-8.  Used for HA discovery to have correct min value in UI |
| ENABLE_RAW      |          | false   | enable sending raw events to mqtt, otherwise only known changes are sent |
| ENABLE_MONITOR  |          | false   | enable monitor mode which accepts messages addressed to any device. May be useful when ENABLE_WRITE = false |
| OBJECT_ID       |          | true    | Send object_id with HA Auto Discovery for HA entity names |
| NEW_PROTOCOL    |          | false   | Use different registers for newer devices |

## Multiple Devices

Running multiple devices is supported (although not tested).  Currently this requires
running own process for each device.  DEVICE_ID and DEVICE_NAME shoud be set uniquely for each device, like DEVICE_ID=vallox1, DEVICE_NAME="Vallox 1" for one device and DEVICE_ID=vallox2, DEVICE_NAME="Vallox 2" for other device.

## Usage

For example with following script
```sh
#!/bin/sh

# Change to your real rs485 device
export SERIAL_DEVICE=/dev/ttyUSB0
# Change to your real mqtt url
export MQTT_URL=tcp://localhost:8883
# Set device id and name, in case of multiple devices
export DEVICE_ID=valloxupstairs
export DEVICE_NAME="Vallox Upstairs"

./vallox-mqtt
```

## MQTT Topics used

With default configuration:
- homeassistant/status subscribe to HA status changes
- vallox/fan/set subscribe to fan speed commands
- vallox/fan/speed publish fan speeds
- vallox/temp/incoming/outside Outdoor temperature
- vallox/temp/incoming/inside Incoming temperature
- vallox/temp/outgoing/inside Inside temperature
- vallox/temp/outgoing/outside Exhaust temperature
- vallox/temp/insidetarget Post-heating target temperature
- vallox/temp/hexbypass Heat exchanger by-pass temperature (outside)
- vallox/temp/postheating Post-heating setpoint temperature
- vallox/lights Indicator lights as seen on the panel (see below)
- vallox/errorcode Active error code (see below)
- vallox/misc/ioport I/O port status (see below)
- vallox/misc/flags2 2nd register flags (see below)
- vallox/misc/flags6 6th register flags (see below)
- vallox/raw/# Raw register value changes (if raw values are enabled)

If DEVICE_ID is specified it is used as mqtt base topic, for example if DEVICE_ID=vallox1 then topics would be:
- vallox1/fan/set subscribe to fan speed commands
- vallox1/fan/speed publish fan speeds
- vallox1/temperature_incoming_outside Outdoor temperature
- vallox1/temperature_incoming_inside Incoming temperature
- vallox1/temperature_outgoing_inside Inside temperature
- vallox1/temperature_outgoing_outside Exhaust temperature
- vallox1/raw/# Raw register value changes (if raw values are enabled)

And so on.

## Home Assistant sensors

If mqtt auto discovery is used and OBJECT_ID is true (default) Home Assistant sensors are created based on DEVICE_ID like:
- sensor.vallox_fan_speed
- select.vallox_fan_select
- sensor.vallox_temp_incoming_outside
- sensor.vallox_temp_incoming_insise
- sensor.vallox_temp_outgoing_inside
- sensor.vallox_temp_outgoing_outside

Without OBJECT_ID sensor ids are automatically created by HA based on sensor names

## Additional information

### Error codes

Possible error codes are listed here.
 - 05H: Incoming (inside) air temperature sensor fault
 - 06H: CO2 alarm
 - 07H: Outdoor (incoming) air temperature sensor fault
 - 08H: Inside (exhaust) air temperature sensor fault
 - 09H: Water coil freezing risk warning
 - 0AH: Exhaust (outside) air temperature sensor fault

### Indicator lights

Indicator lights are a bitmask where every bit 1 indicates that certain light on the control panel is lighted.
 - bit 0 (LSB): Power light
 - bit 1: CO2 sensor light
 - bit 2: RH (humidity) sensor light
 - bit 3: Post-heating light
 - bits 4-6: N/A
 - bit 7 (MSB): N/A

### 2nd register flags

This register contains miscellancelous flags that are usefull for monitoring.
 - bit 0 (LSB): Speed up request from CO2 sensor
 - bit 1: Speed down request from CO2 sensor
 - bit 2: Speed down request from RH% sensor
 - bit 3: Speed down request by switch
 - bit 6: CO2 alarm
 - bit 7 (MSB): Heat exchanger risk of freezing alarm

### 6th register flags

This register contains miscellancelous flags that are usefull for monitoring.
 - bit 0 (LSB): N/A
 - bits 1-3: N/A
 - bit 4: Remote control enabled
 - bit 5: N/A
 - bit 6: Fireplace mode (or boosting mode?) on
 - bit 7 (MSB): N/A

### I/O port

This register contains I/O port (register 08H) status
 - bit 0 (LSB): N/A
 - bit 1: Hex bypass state (0 = winter, 1 = summer)
 - bit 2: Error relay state (0 = open, 1 = closed)
 - bit 3: Input fan status (0 = on, 1 = off)
 - bit 4: Pre-heating (0 = off, 1 = on)
 - bit 5: Exhaust fan status (0 = on, 1 = off)
 - bit 6: Fireplace/boost switch (0 = open, 1 = closed)
 - bit 7 (MSB): N/A
