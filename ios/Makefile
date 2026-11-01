# terminatr for iPhone (T788): Terminatr.xcodeproj, its sources in
# Terminatr/ (folder-synchronized: a file added there needs no project
# edit), TerminatrKit and TerminatrUI from the Swift package in ../macos.
# Xcode 27.1 or later (the iOS 27.1 SDK: iPhone Duo, README.md), with
# an iOS Simulator runtime (xcodebuild -downloadPlatform iOS); no other
# tools.
#
#   make build   build for the iOS Simulator (unsigned); ARCHS=arm64
#                builds only that one (CI)
#   make run     build, install and open it in the Simulator DEVICE
#                (a name or UDID; booted if it isn't), in demo mode
#   make dev-server  a dev tm server and relay for the app to pair and
#                link with (internal/e2e TestPhoneDev; Go, the fake agent),
#                seeded in about 15s with what the screens show:
#                  acme-web (pinned): a coordinator with 30 turns (more
#                    than a page: Earlier messages), 22 tasks across Done,
#                    In motion, review, On deck, Backlog and blocked, with
#                    steps and notes; threads asking a question (T13),
#                    working (T14), blocked on a permission prompt (T15)
#                    and idle with a report and attachment, in review
#                    (T16); CONTEXT.md, MEMORY.md, 3 memory notes; inbox
#                    items; linked both ways with billing-api, whose
#                    message M-1 brings a file
#                  billing-api: a coordinator, a thread idle with a
#                    report and attachment (T3), one working (T4)
#                  docs-site: a coordinator, 4 tasks, a question for you
#                  old-prototype: inactive, its tasks done
#                It writes its pairing code to $(DEV)/invite.txt and the
#                server's TERMINATR_HOME/SOCKET to $(DEV)/home.txt (source
#                it for tm commands, or the Mac app, against it); touch
#                $(DEV)/asleep to stop the server, rm it to start it
#                again (the agents resume, idle), touch $(DEV)/stop to end;
#                LEVEL=write pairs the phone at write (its prompt box and
#                its terminal's keys), else at answer
#   make run-paired  as run, pairing with dev-server's code instead of
#                the demo (the Simulator has no camera)
#   make archive the Release build for devices, in $(ARCHIVE):
#                VERSION and BUILD set its version (0.54.0) and build
#                number; without PROFILE (an App Store provisioning
#                profile's UUID, installed) it is unsigned, a dry run;
#                with it signed by an Apple Distribution identity in the
#                keychain, of the Release config's team (DEVELOPMENT_TEAM,
#                terminatr's; TEAM overrides it). scripts/release/ios.sh, which
#                the release workflow runs, exports and uploads it
#                (README.md, TestFlight)
#   make store-text  check the App Store text in AppStore/ against App
#                Store Connect's field limits (README.md, App Store)
#
# The app's warnings are errors (SWIFT_TREAT_WARNINGS_AS_ERRORS in the
# project). The package's aren't here: Xcode 26 builds it as a dependency
# with -suppress-warnings, which TERMINATR_WERROR's -warnings-as-errors
# conflicts with, so the build unsets it; macos/Makefile keeps its
# warnings errors. Builds in Xcode sign automatically; the Release config
# names terminatr's team (4689D3AT8J, the Mac app's too), Debug none, so
# a device build in Xcode takes your own.

DEVICE       ?= iPhone 17
CONFIG       ?= Debug
DERIVED      ?= build/DerivedData
APP          := $(DERIVED)/Build/Products/$(CONFIG)-iphonesimulator/Terminatr.app
BUNDLE_ID    := dev.terminatr.ios
DEV          ?= build/dev
ARCHIVE      ?= build/Terminatr.xcarchive
VERSION      ?= 0.0.0
BUILD        ?= 1
# The Release config's team, passed to every target when signing: the
# packages' resource bundles are signed too, and don't read the app's.
TEAM         ?= $(shell sed -n 's/^[[:space:]]*DEVELOPMENT_TEAM = \([A-Z0-9]*\);$$/\1/p' Terminatr.xcodeproj/project.pbxproj)
unexport TERMINATR_WERROR

.PHONY: build run dev-server run-paired archive store-text clean

# The scheme is Terminatr-iOS (xcshareddata): a scheme named Terminatr
# can resolve to the package's Mac executable of that name, which then
# builds for iOS and fails (on SwiftTerm's plugin, then on AppKit).
# SwiftTerm (the Terminal screen, T840) runs a build-tool plugin that
# writes its version into the build; xcodebuild asks to trust a plugin
# in Xcode's dialog first, which a command line can't, so it skips that.
PLUGINS      := -skipPackagePluginValidation

build:
	xcodebuild -project Terminatr.xcodeproj -scheme Terminatr-iOS -configuration $(CONFIG) $(PLUGINS) \
		-destination 'generic/platform=iOS Simulator' -derivedDataPath $(DERIVED) $(if $(ARCHS),ARCHS="$(ARCHS)") build

run: build
	xcrun simctl boot "$(DEVICE)" 2>/dev/null || true
	xcrun simctl install "$(DEVICE)" $(APP)
	xcrun simctl launch "$(DEVICE)" $(BUNDLE_ID) -demo YES

dev-server:
	mkdir -p $(DEV) && rm -f $(DEV)/invite.txt $(DEV)/asleep $(DEV)/stop
	cd .. && E2E=1 TM_PHONE_DEV_DIR="$(abspath $(DEV))" TM_PHONE_DEV_LEVEL="$(LEVEL)" go test -count=1 -v -timeout 0 -run '^TestPhoneDev$$' ./internal/e2e

run-paired: build
	xcrun simctl boot "$(DEVICE)" 2>/dev/null || true
	xcrun simctl install "$(DEVICE)" $(APP)
	xcrun simctl launch "$(DEVICE)" $(BUNDLE_ID) -pair "$$(cat $(DEV)/invite.txt)"

# Signed, the profile goes to the app target only (TM_IOS_PROFILE, read
# by its Release PROVISIONING_PROFILE_SPECIFIER): set for every target,
# the packages' resource bundles refuse it.
archive:
	xcodebuild -project Terminatr.xcodeproj -scheme Terminatr-iOS -configuration Release $(PLUGINS) \
		-destination 'generic/platform=iOS' -derivedDataPath $(DERIVED) -archivePath $(ARCHIVE) \
		MARKETING_VERSION=$(VERSION) CURRENT_PROJECT_VERSION=$(BUILD) \
		$(if $(PROFILE),CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY="Apple Distribution" DEVELOPMENT_TEAM=$(TEAM) TM_IOS_PROFILE=$(PROFILE),CODE_SIGNING_ALLOWED=NO) \
		archive

store-text:
	AppStore/check.sh

clean:
	rm -rf build
