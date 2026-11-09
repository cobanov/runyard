plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.plugin.compose")
}

val appVersion = "0.6.0"
val versionParts = appVersion.split('.').map { it.toInt() }

android {
    namespace = "dev.cobanov.runyard"
    compileSdk = 36

    defaultConfig {
        applicationId = "dev.cobanov.runyard"
        minSdk = 29
        targetSdk = 36
        versionCode = versionParts[0] * 10000 + versionParts[1] * 100 + versionParts[2]
        versionName = appVersion
    }

    // Releases are signed with the key android/release.sh hands over in the environment; it never lives in
    // the repository. Without it a release build comes out unsigned.
    val keystore = System.getenv("RUNYARD_KEYSTORE")
    signingConfigs {
        create("release") {
            if (keystore != null) {
                storeFile = file(keystore)
                storePassword = System.getenv("RUNYARD_KEYSTORE_PASSWORD")
                keyAlias = "runyard"
                keyPassword = System.getenv("RUNYARD_KEYSTORE_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"))
            if (keystore != null) signingConfig = signingConfigs.getByName("release")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures { compose = true }
}

dependencies {
    val bom = platform("androidx.compose:compose-bom:2026.06.01")
    implementation(bom)
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.material:material-icons-core:1.7.8")
    implementation("androidx.activity:activity-compose:1.13.0")
    debugImplementation("androidx.compose.ui:ui-tooling")
    // Android's org.json is a stub on the JVM; the tests use the real one.
    testImplementation("org.json:json:20260814")
    testImplementation("junit:junit:4.13.2")
}
