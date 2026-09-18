import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    alias(libs.plugins.kotlinMultiplatform)
    alias(libs.plugins.androidApplication)
    alias(libs.plugins.composeMultiplatform)
    alias(libs.plugins.composeCompiler)
    alias(libs.plugins.kotlinSerialization)
}

kotlin {
    androidTarget {
        compilerOptions {
            jvmTarget.set(JvmTarget.JVM_11)
        }
    }

    listOf(
        iosX64(),
        iosArm64(),
        iosSimulatorArm64(),
    ).forEach { iosTarget ->
        iosTarget.binaries.framework {
            baseName = "ComposeApp"
            isStatic = true
        }
    }

    sourceSets {
        commonMain.dependencies {
            implementation(compose.runtime)
            implementation(compose.foundation)
            implementation(compose.ui)
            implementation(compose.material3)
            implementation(compose.materialIconsExtended)
            implementation(libs.kotlinx.coroutines.core)
            implementation(libs.kotlinx.serialization.json)
            implementation(libs.ktor.client.core)
            implementation(libs.ktor.client.content.negotiation)
            implementation(libs.ktor.serialization.kotlinx.json)
            implementation(libs.coil.compose)
            implementation(libs.coil.network.ktor3)
        }
        androidMain.dependencies {
            // Ktor engine + Android-specific platform APIs.
            implementation(libs.ktor.client.okhttp)
            implementation(libs.androidx.activity.compose)
            implementation(libs.media3.exoplayer)
        }
        iosMain.dependencies {
            implementation(libs.ktor.client.darwin)
        }
    }
}

android {
    namespace = "com.music.app"
    compileSdk = 37

    defaultConfig {
        manifestPlaceholders += mapOf()
        applicationId = "com.music.app"
        minSdk = 27
        targetSdk = 37
        versionCode = 1
        versionName = "0.1.0"

        // Host that shared playlist links use, claimed by the deep-link
        // intent-filter in AndroidManifest.xml. The server is self-hosted, so
        // set this to your own domain:
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
    compileSdkMinor = 0
}
