# terminatr for Mac (SwiftPM, macOS 14+).
#
#   make build   swift build (debug)
#   make test    swift test
#   make app     build/Terminatr.app, unsigned (signing comes with the release job)
#   make run     make app, then open it
#
# With only the Command Line Tools (no Xcode), Swift Testing's macros sit
# in a plugin directory swift test doesn't search: TEST_FLAGS adds it.
# The macOS 27 SDK's SwiftUI makes @State a macro whose plugin comes only
# with Xcode: there, build against the 26 SDK, e.g.
#   SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.sdk make app
# SwiftTerm (the terminal tab) compiles a Metal shader, so building needs
# Xcode with its Metal toolchain (xcodebuild -downloadComponent
# MetalToolchain); the Command Line Tools alone have no metal.

SWIFT        ?= swift
CONFIG       ?= release
CLT          := /Library/Developer/CommandLineTools
TEST_FLAGS   := $(if $(filter $(CLT)%,$(shell xcode-select -p 2>/dev/null)),-Xswiftc -plugin-path -Xswiftc $(CLT)/usr/lib/swift/host/plugins/testing)
APP          := build/Terminatr.app

.PHONY: build test app run clean

build:
	$(SWIFT) build

test:
	$(SWIFT) test $(TEST_FLAGS)

app:
	$(SWIFT) build -c $(CONFIG)
	rm -rf $(APP)
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
	cp "$$($(SWIFT) build -c $(CONFIG) --show-bin-path)/Terminatr" $(APP)/Contents/MacOS/
	cp -R "$$($(SWIFT) build -c $(CONFIG) --show-bin-path)/Terminatr_Terminatr.bundle" $(APP)/Contents/Resources/
	cp Info.plist $(APP)/Contents/Info.plist
	plutil -lint $(APP)/Contents/Info.plist

run: app
	open $(APP)

clean:
	rm -rf .build build
