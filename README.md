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

Might work with other Vallox devices with rs485 bus.  There probably are some differences between different devices.  If there are those probably are easy to adapt to.

The application itself has been tested running on Raspberry Pi 3, but probably works just fine with Raspberry Zero or anything running linux.

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
| MQTT_CLIENT_ID  |          | vallox  | mqtt client id |
| DEBUG           |          | false   | enable debug output, true/false |
| ENABLE_WRITE    |          | false   | enable sending commands/writing to bus, true/false |
| SPEED_MIN       |          | 1       | minimum speed for the device, between 1-8.  Used for HA discovery to have correct min value in UI |
| ENABLE_RAW      |          | false   | enable sending raw events to mqtt, otherwise only known changes are sent |

## Usage

For example with following script
```sh
#!/bin/sh

# Change to your real rs485 device
export SERIAL_DEVICE=/dev/ttyUSB0
# Change to your real mqtt url
export MQTT_URL=tcp://localhost:8883

./vallox-mqtt
```

## MQTT Topics used

- homeassistant/status subscribe to HA status changes
- vallox/fan/set subscribe to fan speed commands
- vallox/fan/speed publish fan speeds
- vallox/temp/incoming/outside Outdoor temperature
- vallox/temp/incoming/inside Incoming temperature
- vallox/temp/outgoing/inside Inside temperature
- vallox/temp/outgoing/outside Exhaust temperature
- vallox/lights Indicator lights as seen on the panel (see below)
- vallox/misc/flags6 6th register flags (see below)
- vallox/raw/# Raw register value changes (if raw values are enabled)

### Indicator lights
Indicator lights are a bitmask where every bit 1 indicates that certain light on the control panel is lighted.
 - bit 0 (LSB): Power light
 - bit 1: CO2 sensor light
 - bit 2: RH (humidity) sensor light
 - bit 3: Post-heating light
 - bits 4-6: N/A
 - bit 7 (MSB): N/A

### 6th register flags
This register contains miscellancelous flags that are usefull for monitoring.
 - bit 0 (LSB): N/A
 - bits 1-3: N/A
 - bit 4: Remote control enabled
 - bit 5: N/A
 - bit 6: Fireplace mode (or boosting mode?) on
 - bit 7 (MSB): N/A
