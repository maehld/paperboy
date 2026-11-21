module github.com/maehld/paperboy/server

go 1.27.0

require (
	github.com/maehld/paperboy/sdk v0.0.0
	go.yaml.in/yaml/v3 v3.0.5
	golang.org/x/image v0.24.0
)

require golang.org/x/text v0.22.0 // indirect

replace github.com/maehld/paperboy/sdk => ../sdk
