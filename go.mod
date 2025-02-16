module vallox-mqtt

go 1.17

replace github.com/pvainio/vallox-rs485 => github.com/zanppa/vallox-rs485

require (
	github.com/eclipse/paho.mqtt.golang v1.3.5
	github.com/pvainio/vallox-rs485 v0.0.3
)

require (
	github.com/gorilla/websocket v1.4.2 // indirect
	golang.org/x/net v0.0.0-20211020060615-d418f374d309 // indirect
)

require (
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/tarm/serial v0.0.0-20180830185346-98f6abe2eb07 // indirect
	golang.org/x/sys v0.0.0-20211025201205-69cdffdb9359 // indirect
)
