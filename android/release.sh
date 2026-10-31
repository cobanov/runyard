#!/bin/sh
# Builds the signed Android app into android/build/dist/runyard-<version>.apk.
#
# The release key is ~/.config/runyard/android-release.keystore (or $RUNYARD_KEYSTORE), alias runyard, its
# password in ~/.config/runyard/android-keystore-password (or $RUNYARD_KEYSTORE_PASSWORD). Keep a copy of both:
# an app signed with another key cannot update one already installed.
set -e

ANDROID=$(cd "$(dirname "$0")" && pwd)
VERSION=$(sed -n 's/^val appVersion = "\(.*\)"/\1/p' "$ANDROID/app/build.gradle.kts")
# Gradle 9 runs on JDK 17 to 25; Homebrew's own openjdk may be newer.
[ -x "$JAVA_HOME/bin/java" ] || JAVA_HOME=/opt/homebrew/opt/openjdk@21
export JAVA_HOME
export ANDROID_HOME=${ANDROID_HOME:-/opt/homebrew/share/android-commandlinetools}
export RUNYARD_KEYSTORE=${RUNYARD_KEYSTORE:-$HOME/.config/runyard/android-release.keystore}
[ -f "$RUNYARD_KEYSTORE" ] || { echo "No release key at $RUNYARD_KEYSTORE"; exit 1; }
PASSWORD_FILE=$HOME/.config/runyard/android-keystore-password
RUNYARD_KEYSTORE_PASSWORD=${RUNYARD_KEYSTORE_PASSWORD:-$(cat "$PASSWORD_FILE")}
export RUNYARD_KEYSTORE_PASSWORD

(cd "$ANDROID" && ./gradlew --quiet clean testDebugUnitTest assembleRelease)
OUT="$ANDROID/build/dist"
APK="$OUT/runyard-$VERSION.apk"
mkdir -p "$OUT"
cp "$ANDROID/app/build/outputs/apk/release/app-release.apk" "$APK"
echo "$APK"
echo "sha256: $(shasum -a 256 "$APK" | cut -d' ' -f1)"
