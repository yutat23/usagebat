BINARY := usage-battery
APP := build/UsageBattery.app

.PHONY: all build test vet run bundle windows clean

all: test build

build:
	go build -o build/$(BINARY) ./cmd/usage-battery

test:
	go test ./...

vet:
	go vet ./...

# Run in the foreground; Ctrl-C to stop.
run: build
	./build/$(BINARY)

# Print what the app currently reads, and dump the icon it would draw.
dump: build
	./build/$(BINARY) -dump build/icon.png

# A minimal .app bundle. macOS is happier launching menu-bar apps this way, and
# it is what a login item needs to point at.
bundle: build
	rm -rf $(APP)
	mkdir -p $(APP)/Contents/MacOS
	cp build/$(BINARY) $(APP)/Contents/MacOS/$(BINARY)
	printf '%s\n' \
		'<?xml version="1.0" encoding="UTF-8"?>' \
		'<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' \
		'<plist version="1.0"><dict>' \
		'  <key>CFBundleName</key><string>Usage Battery</string>' \
		'  <key>CFBundleIdentifier</key><string>dev.yutat23.usage-battery</string>' \
		'  <key>CFBundleExecutable</key><string>$(BINARY)</string>' \
		'  <key>CFBundlePackageType</key><string>APPL</string>' \
		'  <key>CFBundleShortVersionString</key><string>0.1.0</string>' \
		'  <key>LSUIElement</key><true/>' \
		'</dict></plist>' > $(APP)/Contents/Info.plist
	@echo "built $(APP)"

# -H windowsgui keeps a console window from opening behind the tray icon.
windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
		go build -ldflags "-H windowsgui" -o build/$(BINARY).exe ./cmd/usage-battery

clean:
	rm -rf build
