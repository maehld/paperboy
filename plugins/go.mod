module github.com/maehld/paperboy/plugins

go 1.27.0

require (
	github.com/maehld/paperboy/sdk v0.0.0
	golang.org/x/image v0.24.0
)

require golang.org/x/text v0.22.0 // indirect

replace github.com/maehld/paperboy/sdk => ../sdk
