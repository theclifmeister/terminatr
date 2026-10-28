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
#   make dev-server  a dev tm server and relay for the app to pair and
#                link with (internal/appapi TestPhoneDev; Go): it writes
#                its pairing code to $(DEV)/invite.txt; touch
#                $(DEV)/asleep to stop the server, rm it to start it
#                again, touch $(DEV)/stop to end
#   make run-paired  as run, pairing with dev-server's code instead of
#                the demo (the Simulator has no camera)
#   make archive the Release build for devices, in $(ARCHIVE):
#                VERSION and BUILD set its version (0.54.0) and build
#                number; without PROFILE (an App Store provisioning
#                profile's UUID, installed) and TEAM it is unsigned, a
#                dry run; with them signed by an Apple Distribution
#                identity in the keychain. scripts/release/ios.sh, which
#                the release workflow runs, exports and uploads it
#                (README.md, TestFlight)
#
# The app's warnings are errors (SWIFT_TREAT_WARNINGS_AS_ERRORS in the
# project). The package's aren't here: Xcode 26 builds it as a dependency
# with -suppress-warnings, which TERMINATR_WERROR's -warnings-as-errors
# conflicts with, so the build unsets it; macos/Makefile keeps its
# warnings errors. Local builds in Xcode sign automatically with your
# own team; the project holds none.

DEVICE       ?= iPhone 17
CONFIG       ?= Debug
DERIVED      ?= build/DerivedData
APP          := $(DERIVED)/Build/Products/$(CONFIG)-iphonesimulator/Terminatr.app
BUNDLE_ID    := dev.terminatr.ios
DEV          ?= build/dev
ARCHIVE      ?= build/Terminatr.xcarchive
VERSION      ?= 0.0.0
BUILD        ?= 1
unexport TERMINATR_WERROR

.PHONY: build run dev-server run-paired archive clean

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

dev-server:
	mkdir -p $(DEV) && rm -f $(DEV)/invite.txt $(DEV)/asleep $(DEV)/stop
	cd .. && TM_PHONE_DEV_DIR="$(abspath $(DEV))" go test -count=1 -v -timeout 0 -run '^TestPhoneDev$$' ./internal/appapi

run-paired: build
	xcrun simctl boot "$(DEVICE)" 2>/dev/null || true
	xcrun simctl install "$(DEVICE)" $(APP)
	xcrun simctl launch "$(DEVICE)" $(BUNDLE_ID) -pair "$$(cat $(DEV)/invite.txt)"

# Signed, the profile goes to the app target only (TM_IOS_PROFILE, read
# by its Release PROVISIONING_PROFILE_SPECIFIER): set for every target,
# the packages' resource bundles refuse it.
archive:
	xcodebuild -project Terminatr.xcodeproj -scheme Terminatr-iOS -configuration Release \
		-destination 'generic/platform=iOS' -derivedDataPath $(DERIVED) -archivePath $(ARCHIVE) \
		MARKETING_VERSION=$(VERSION) CURRENT_PROJECT_VERSION=$(BUILD) \
		$(if $(PROFILE),CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY="Apple Distribution" DEVELOPMENT_TEAM=$(TEAM) TM_IOS_PROFILE=$(PROFILE),CODE_SIGNING_ALLOWED=NO) \
		archive

clean:
	rm -rf build
