BINARY := usage-battery
APP := build/UsageBattery.app
VERSION ?= 0.1.0
VERSION_PKG := github.com/yutat23/usage-battery/internal/version
VERSION_LDFLAGS := -X $(VERSION_PKG).Value=$(VERSION)

.PHONY: all build test vet run bundle windows release clean

all: test build

build:
	MACOSX_DEPLOYMENT_TARGET=11.0 go build -ldflags "$(VERSION_LDFLAGS)" -o build/$(BINARY) ./cmd/usage-battery

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
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
	cp build/$(BINARY) $(APP)/Contents/MacOS/$(BINARY)
	cp LICENSE $(APP)/Contents/Resources/LICENSE.txt
	sed 's/@VERSION@/$(VERSION)/g' packaging/Info.plist > $(APP)/Contents/Info.plist
	codesign --force --deep --sign - $(APP)
	@echo "built $(APP)"

# -H windowsgui keeps a console window from opening behind the tray icon.
windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
		go build -ldflags "-H windowsgui $(VERSION_LDFLAGS)" -o build/$(BINARY).exe ./cmd/usage-battery

release:
	./scripts/build-release.sh $(VERSION)

clean:
	rm -rf build
