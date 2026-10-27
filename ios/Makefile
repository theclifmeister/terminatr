# terminatr for iPhone (T788): Terminatr.xcodeproj, its sources in
# Terminatr/ (folder-synchronized: a file added there needs no project
# edit), TerminatrKit and TerminatrUI from the Swift package in ../macos.
# Xcode 16 or later, with an iOS Simulator runtime
# (xcodebuild -downloadPlatform iOS); no other tools.
#
#   make build   build for the iOS Simulator (unsigned); ARCHS=arm64
#                builds only that one (CI)
#   make run     build, install and open it in the Simulator DEVICE
#                (a name or UDID; booted if it isn't), in demo mode
#
# The app's warnings are errors (SWIFT_TREAT_WARNINGS_AS_ERRORS in the
# project). The package's aren't here: Xcode 26 builds it as a dependency
# with -suppress-warnings, which TERMINATR_WERROR's -warnings-as-errors
# conflicts with, so the build unsets it; macos/Makefile keeps its
# warnings errors. Signing, TestFlight and a device build come later:
# nothing here signs for a device or holds a team.

DEVICE       ?= iPhone 17
CONFIG       ?= Debug
DERIVED      ?= build/DerivedData
APP          := $(DERIVED)/Build/Products/$(CONFIG)-iphonesimulator/Terminatr.app
BUNDLE_ID    := dev.terminatr.ios
unexport TERMINATR_WERROR

.PHONY: build run clean

# The scheme is Terminatr-iOS (xcshareddata): a scheme named Terminatr
# can resolve to the package's Mac executable of that name, which then
# builds for iOS and fails (on SwiftTerm's plugin, then on AppKit).
build:
	xcodebuild -project Terminatr.xcodeproj -scheme Terminatr-iOS -configuration $(CONFIG) \
		-destination 'generic/platform=iOS Simulator' -derivedDataPath $(DERIVED) $(if $(ARCHS),ARCHS="$(ARCHS)") build

run: build
	xcrun simctl boot "$(DEVICE)" 2>/dev/null || true
	xcrun simctl install "$(DEVICE)" $(APP)
	xcrun simctl launch "$(DEVICE)" $(BUNDLE_ID) -demo YES

clean:
	rm -rf build
