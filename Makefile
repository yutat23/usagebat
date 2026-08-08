BINARY := usagebat
APP := build/usagebat.app
VERSION ?= 0.6.0
VERSION_PKG := github.com/yutat23/usagebat/internal/version
# -s -w drop the symbol table and DWARF, which is about a third of the binary.
# Go builds its stack traces from its own runtime tables, so panics still name
# their functions; only attaching a debugger loses anything.
VERSION_LDFLAGS := -s -w -X $(VERSION_PKG).Value=$(VERSION)

.PHONY: all build test vet run icons bundle windows release clean

all: test build

build:
	MACOSX_DEPLOYMENT_TARGET=11.0 go build -ldflags "$(VERSION_LDFLAGS)" -o build/$(BINARY) ./cmd/usagebat

test:
	go test ./...

vet:
	go vet ./...

# Run in the foreground; Ctrl-C to stop.
run: build
	./build/$(BINARY) --foreground

icons:
	./scripts/generate-icons.sh

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
	cp packaging/usagebat.icns $(APP)/Contents/Resources/usagebat.icns
	sed 's/@VERSION@/$(VERSION)/g' packaging/Info.plist > $(APP)/Contents/Info.plist
	codesign --force --deep --sign - $(APP)
	@echo "built $(APP)"

# -H windowsgui keeps a console window from opening behind the tray icon.
windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
		go build -ldflags "-H windowsgui $(VERSION_LDFLAGS)" -o build/$(BINARY).exe ./cmd/usagebat

release:
	./scripts/build-release.sh $(VERSION)

clean:
	rm -rf build
