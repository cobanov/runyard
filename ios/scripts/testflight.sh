#!/bin/sh
# Builds runyard for the App Store and uploads it to TestFlight, signed with an App Store Connect API key
# (no Apple ID or 2FA). Adapted from PocketVibe's.
#
#   ASC_KEY_ID=... ASC_ISSUER_ID=... APPLE_TEAM_ID=... ASC_SIGNING_CERT=<sha1> sh ios/scripts/testflight.sh
#   ... sh ios/scripts/testflight.sh --no-upload    # stop at the .ipa
#
# Signing is manual: the Apple Distribution certificate ASC_SIGNING_CERT (SHA-1) and the "runyard App Store"
# profile (swift scripts/asc.swift profile <sha1> makes it). Set SIGNING_KEYCHAIN, with its password in
# SIGNING_KEYCHAIN_PASSWORD_FILE, where no one can type the login keychain's password (SSH, an agent).
#
# The build number counts the repository's commits, and App Store Connect takes each number once:
# commit before uploading again.
set -e

IOS=$(cd "$(dirname "$0")/.." && pwd)
: "${ASC_KEY_ID:?set ASC_KEY_ID}" "${ASC_ISSUER_ID:?set ASC_ISSUER_ID}" "${APPLE_TEAM_ID:?set APPLE_TEAM_ID}"
: "${ASC_SIGNING_CERT:?set ASC_SIGNING_CERT (SHA-1 of an Apple Distribution certificate)}"
BUILD="$IOS/build"
ARCHIVE="$BUILD/Runyard.xcarchive"
EXPORT="$BUILD/export"
NUMBER=$(git -C "$IOS" rev-list --count HEAD)

if [ -n "$SIGNING_KEYCHAIN" ]; then
  [ -n "$SIGNING_KEYCHAIN_PASSWORD_FILE" ] && security unlock-keychain -p "$(cat "$SIGNING_KEYCHAIN_PASSWORD_FILE")" "$SIGNING_KEYCHAIN"
  # Export signs through the search list, so the keychain joins it for this run only.
  OLD_LIST=$(security list-keychains -d user | tr -d '"')
  # shellcheck disable=SC2086
  security list-keychains -d user -s "$SIGNING_KEYCHAIN" $OLD_LIST
  # shellcheck disable=SC2086
  trap 'security list-keychains -d user -s $OLD_LIST' EXIT
fi

rm -rf "$ARCHIVE" "$EXPORT"
cd "$IOS" && xcodegen generate --quiet
xcodebuild archive -quiet \
  -project Runyard.xcodeproj -scheme Runyard -configuration Release \
  -destination 'generic/platform=iOS' -archivePath "$ARCHIVE" \
  CURRENT_PROJECT_VERSION="$NUMBER" DEVELOPMENT_TEAM="$APPLE_TEAM_ID" \
  CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY="$ASC_SIGNING_CERT" PROVISIONING_PROFILE_SPECIFIER="runyard App Store" \
  ${SIGNING_KEYCHAIN:+OTHER_CODE_SIGN_FLAGS="--keychain $SIGNING_KEYCHAIN"} \
  COMPILER_INDEX_STORE_ENABLE=NO

cat > "$BUILD/ExportOptions.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>method</key><string>app-store-connect</string>
  <key>teamID</key><string>$APPLE_TEAM_ID</string>
  <key>signingStyle</key><string>manual</string>
  <key>signingCertificate</key><string>$ASC_SIGNING_CERT</string>
  <key>provisioningProfiles</key>
  <dict><key>dev.cobanov.runyard</key><string>runyard App Store</string></dict>
  <key>destination</key><string>export</string>
  <key>manageAppVersionAndBuildNumber</key><false/>
</dict>
</plist>
PLIST
# No API key here: with one, export takes the cloud signing path, which this key may not be allowed.
# Apple's rsync first: Homebrew's rejects the flags export copies with ("exportArchive Copy failed").
PATH="/usr/bin:$PATH" xcodebuild -exportArchive -quiet -archivePath "$ARCHIVE" -exportPath "$EXPORT" -exportOptionsPlist "$BUILD/ExportOptions.plist"
IPA="$EXPORT/Runyard.ipa"
echo "runyard $(unzip -p "$IPA" Payload/Runyard.app/Info.plist | plutil -extract CFBundleShortVersionString raw -) ($NUMBER): $IPA"

[ "$1" = "--no-upload" ] && exit 0
xcrun altool --upload-app -f "$IPA" -t ios --apiKey "$ASC_KEY_ID" --apiIssuer "$ASC_ISSUER_ID"
echo "Uploaded. It shows in TestFlight once App Store Connect has processed it (a few minutes)."
