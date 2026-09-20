import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    alias(libs.plugins.kotlinMultiplatform)
    alias(libs.plugins.androidKmpLibrary)
    alias(libs.plugins.composeMultiplatform)
    alias(libs.plugins.composeCompiler)
    alias(libs.plugins.kotlinSerialization)
}

// This module is the shared code — commonMain, iosMain and the Android
// implementation (Activity, playback service, widget) as an Android *library*.
// The installable APK is built by :androidApp, which does nothing but depend on
// this module and hold app-level settings (applicationId, signing, deep-link
// host). AGP 9 dropped support for com.android.application inside a Kotlin
// Multiplatform module, which is why the two are separate.
kotlin {
    android {
        namespace = "com.music.app"
        compileSdk = 37
        minSdk = 27
        // The launcher icon, theme and widget definition live in androidMain/res.
        androidResources.enable = true
        compilerOptions {
            jvmTarget.set(JvmTarget.JVM_11)
        }
    }

    // TokenStore, PlayerController & co. are expect/actual *classes*, which the
    // compiler still labels Beta; this opts in instead of warning on each one.
    compilerOptions {
        freeCompilerArgs.add("-Xexpect-actual-classes")
    }

    // Compose Multiplatform 1.10+ no longer ships iosX64 artifacts, so the
    // Intel-Mac simulator target is gone; Apple-silicon simulators use
    // iosSimulatorArm64.
    listOf(
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
            implementation(libs.compose.runtime)
            implementation(libs.compose.foundation)
            implementation(libs.compose.ui)
            implementation(libs.compose.material3)
            implementation(libs.compose.material.icons.extended)
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
            implementation(libs.media3.session)
            implementation(libs.glance.appwidget)
            implementation(libs.glance.material3)
        }
        iosMain.dependencies {
            implementation(libs.ktor.client.darwin)
        }
    }
}

