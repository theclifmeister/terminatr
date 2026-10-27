# terminatr for Mac (SwiftPM, macOS 14+).
#
#   make build   swift build (debug)
#   make test    swift test
#   make app     build/Terminatr.app, unsigned (scripts/release/app.sh signs it)
#   make icon-check  Terminatr.app has the icon its Info.plist names
#   make run     make app, then open it
#
# A warning from our targets fails build, test and app (WERROR=1, the
# default): Package.swift makes them errors (-warnings-as-errors, not
# SwiftTerm's), and warnings.sh fails on any a build prints from
# Sources/ or Tests/, as a warning @preconcurrency downgrades (AppKit's
# Sendable callbacks) stays one under -warnings-as-errors. Swift prints
# a warning only when its file compiles: CI's clean build sees them all.
# scripts/release/app.sh builds with WERROR=0.
#
# ARCHS="arm64 x86_64" builds a universal app (the release's): one build
# per architecture, joined by lipo. Not one `swift build --arch arm64
# --arch x86_64`: before Xcode 27 that goes through XCBuild, which can't
# resolve SwiftTerm's build plugin ("missing target ...
# SwiftTermBuildInfoPlugin"). VERSION goes into its Info.plist
# (CFBundleShortVersionString, CFBundleVersion), which tm update reads to
# tell an installed app's release.
#
# With only the Command Line Tools (no Xcode), Swift Testing's macros sit
# in a plugin directory swift test doesn't search: TEST_FLAGS adds it.
# The macOS 27 SDK's SwiftUI makes @State a macro whose plugin comes only
# with Xcode: there, build against the 26 SDK, e.g.
#   SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.sdk make app
# The app icon (Icon/, made by Icon/icon.sh from Icon/AppIcon.icon) is
# copied in as built: AppIcon.icns (CFBundleIconFile, macOS 14 and 15)
# and Assets.car (CFBundleIconName, macOS 26+).
# SwiftTerm (the terminal tab) compiles a Metal shader, so building needs
# Xcode with its Metal toolchain (xcodebuild -downloadComponent
# MetalToolchain); the Command Line Tools alone have no metal.

SWIFT        ?= swift
CONFIG       ?= release
CLT          := /Library/Developer/CommandLineTools
TEST_FLAGS   := $(if $(filter $(CLT)%,$(shell xcode-select -p 2>/dev/null)),-Xswiftc -plugin-path -Xswiftc $(CLT)/usr/lib/swift/host/plugins/testing)
APP          := build/Terminatr.app
ARCHS        ?=
VERSION      ?= 0.0.0
ARCH_DIR     := build/arch
WERROR       ?= 1
export TERMINATR_WERROR := $(WERROR)
CHECK        := $(if $(filter 1,$(WERROR)),./warnings.sh)

.PHONY: build test app icon-check run clean

build:
	$(CHECK) $(SWIFT) build

test:
	$(CHECK) $(SWIFT) test $(TEST_FLAGS)

# Each architecture's binary is copied out as soon as it is built: the
# builds may share one products folder. The resource bundles are the
# same for every architecture; the last build's go in.
app:
	rm -rf $(APP) $(ARCH_DIR)
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
ifeq ($(strip $(ARCHS)),)
	$(CHECK) $(SWIFT) build -c $(CONFIG)
	cp "$$($(SWIFT) build -c $(CONFIG) --show-bin-path)/Terminatr" $(APP)/Contents/MacOS/
	cp -R "$$($(SWIFT) build -c $(CONFIG) --show-bin-path)"/*.bundle $(APP)/Contents/Resources/
else
	set -e; for a in $(ARCHS); do \
		$(CHECK) $(SWIFT) build -c $(CONFIG) --arch $$a; \
		mkdir -p $(ARCH_DIR)/$$a; \
		cp "$$($(SWIFT) build -c $(CONFIG) --arch $$a --show-bin-path)/Terminatr" $(ARCH_DIR)/$$a/; \
		lipo $(ARCH_DIR)/$$a/Terminatr -verify_arch $$a; \
	done; \
	lipo -create -output $(APP)/Contents/MacOS/Terminatr $(foreach a,$(ARCHS),$(ARCH_DIR)/$(a)/Terminatr); \
	cp -R "$$($(SWIFT) build -c $(CONFIG) --arch $(lastword $(ARCHS)) --show-bin-path)"/*.bundle $(APP)/Contents/Resources/
endif
	cp Icon/AppIcon.icns Icon/Assets.car $(APP)/Contents/Resources/
	cp Info.plist $(APP)/Contents/Info.plist
	plutil -replace CFBundleShortVersionString -string $(VERSION) $(APP)/Contents/Info.plist
	plutil -replace CFBundleVersion -string $(VERSION) $(APP)/Contents/Info.plist
	plutil -lint $(APP)/Contents/Info.plist
	$(MAKE) icon-check

# CFBundleIconFile names an .icns in Resources, and CFBundleIconName an
# icon in Resources/Assets.car: otherwise Finder, the Dock and the
# notifications show the generic app.
icon-check:
	@set -e; plist=$(APP)/Contents/Info.plist; res=$(APP)/Contents/Resources; \
	file=$$(plutil -extract CFBundleIconFile raw $$plist); \
	name=$$(plutil -extract CFBundleIconName raw $$plist); \
	[ -f "$$res/$${file%.icns}.icns" ] || { echo "icon-check: no $$res/$${file%.icns}.icns (CFBundleIconFile)" >&2; exit 1; }; \
	xcrun assetutil --info "$$res/Assets.car" | grep -q "\"Name\" : \"$$name\"" || \
		{ echo "icon-check: $$res/Assets.car has no icon $$name (CFBundleIconName)" >&2; exit 1; }; \
	echo "icon-check: ok ($$file.icns, $$name in Assets.car)"

run: app
	open $(APP)

clean:
	rm -rf .build build
