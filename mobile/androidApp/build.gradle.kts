// The Android application shell. All code — including the Android-specific
// parts — lives in :composeApp; this module only exists because AGP 9 no
// longer lets com.android.application share a module with Kotlin
// Multiplatform. What's here is what only an app can decide: its id, version,
// the deep-link host and packaging.
plugins {
    alias(libs.plugins.androidApplication)
}

android {
    namespace = "com.music.app.android"
    compileSdk = 37

    defaultConfig {
        applicationId = "com.music.app"
        minSdk = 27
        targetSdk = 37
        versionCode = 1
        versionName = "0.1.0"

        // Host that shared playlist links use, claimed by the deep-link
        // intent-filter in composeApp's AndroidManifest.xml. The server is
        // self-hosted, so set this to your own domain:
        //
        //   gradle.properties:  musicHost=music.example.com
        //   or on the command line:  ./gradlew assembleDebug -PmusicHost=music.example.com
        //
        // The placeholder must be non-empty for the manifest to merge, hence
        // the example fallback — links only open the app once it matches.
        manifestPlaceholders["musicHost"] =
            (project.findProperty("musicHost") as String?)?.takeIf { it.isNotBlank() }
                ?: "music.cornspies.com"
    }

    buildTypes {
        release {
            isMinifyEnabled = false
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    packaging {
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
        }
    }
    dependenciesInfo {
        includeInBundle = false
        includeInApk = false
    }
}

dependencies {
    implementation(project(":composeApp"))
}
