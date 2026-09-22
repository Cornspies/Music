// The Android application shell. All code — including the Android-specific
// parts — lives in :composeApp; this module only exists because AGP 9 no
// longer lets com.android.application share a module with Kotlin
// Multiplatform. What's here is what only an app can decide: its id, version,
// the deep-link host and packaging.
import com.android.build.api.artifact.SingleArtifact
import java.util.Properties

plugins {
    alias(libs.plugins.androidApplication)
}

// Signing for release builds. An APK has to be signed to install at all, and
// the key must stay the same across versions or Android refuses the update —
// so it lives outside the repository: either mobile/keystore.properties
// (gitignored) or -P flags on the command line. Without it a release build
// still runs, but comes out unsigned and won't install; `releaseApk` says so.
val keystoreProps = Properties().apply {
    val file = rootProject.file("keystore.properties")
    if (file.exists()) file.inputStream().use { load(it) }
}

fun signingValue(key: String): String? =
    (project.findProperty("music.$key") as String?)?.takeIf { it.isNotBlank() }
        ?: keystoreProps.getProperty(key)?.takeIf { it.isNotBlank() }

val keystorePath = signingValue("storeFile")

android {
    namespace = "com.music.app.android"
    compileSdk = 37

    defaultConfig {
        applicationId = "com.music.app"
        minSdk = 27
        targetSdk = 37
        versionCode = 1
        versionName = "0.1.4"

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

    if (keystorePath != null) {
        signingConfigs {
            create("release") {
                storeFile = file(keystorePath)
                storePassword = signingValue("storePassword")
                keyAlias = signingValue("keyAlias")
                keyPassword = signingValue("keyPassword") ?: signingValue("storePassword")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            if (keystorePath != null) signingConfig = signingConfigs.getByName("release")
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

// ---- releaseApk ---------------------------------------------------------------
//
// `./gradlew :androidApp:releaseApk` builds the release APK and copies it to
// mobile/dist/Music-<versionName>.apk — the name the server's download list
// expects, so the file can go straight into its APK directory.
//
// The APK is found through AGP's artifacts API rather than a guessed path,
// so a rename or a layout change in the build directory can't break it.

abstract class PackageReleaseApk : DefaultTask() {
    @get:InputFiles
    abstract val apkDirectory: DirectoryProperty

    @get:Input
    abstract val versionName: Property<String>

    @get:Input
    abstract val signed: Property<Boolean>

    @get:OutputFile
    abstract val outputFile: RegularFileProperty

    @get:Inject
    abstract val files: FileSystemOperations

    @TaskAction
    fun run() {
        val built = apkDirectory.get().asFile.listFiles { f -> f.name.endsWith(".apk") }?.firstOrNull()
            ?: error("the release build produced no APK")
        val target = outputFile.get().asFile
        files.copy {
            from(built)
            into(target.parentFile)
            rename { target.name }
        }
        logger.lifecycle("Release APK: ${target.absolutePath} (version ${versionName.get()})")
        if (!signed.get()) {
            logger.warn(
                "WARNING: this APK is unsigned and will not install. Put a keystore in " +
                    "mobile/keystore.properties (storeFile, storePassword, keyAlias, keyPassword) " +
                    "or pass -Pmusic.storeFile=... and build again.",
            )
        }
    }
}

androidComponents {
    onVariants(selector().withBuildType("release")) { variant ->
        // Name the APK the build itself produces, not a copy of it: that way
        // every route to a release build comes out as Music-<version>.apk —
        // `releaseApk` below, a plain `assembleRelease`, and Android Studio's
        // "Generate Signed Bundle or APK", which copies this file to the
        // folder chosen in its wizard.
        variant.outputs.forEach { output ->
            output.outputFileName.set(
                output.versionName
                    .orElse(android.defaultConfig.versionName ?: "0.0.0")
                    .map { version -> "Music-$version.apk" },
            )
        }

        val packageTask = tasks.register<PackageReleaseApk>("releaseApk") {
            group = "distribution"
            description = "Builds the release APK as mobile/dist/Music-<version>.apk"
            apkDirectory.set(variant.artifacts.get(SingleArtifact.APK))
            versionName.set(android.defaultConfig.versionName ?: "0.0.0")
            signed.set(keystorePath != null)
            outputFile.set(
                rootProject.layout.projectDirectory.file(
                    "dist/Music-${android.defaultConfig.versionName ?: "0.0.0"}.apk",
                ),
            )
        }
        // The artifact wiring above is what makes the build run first.
        packageTask.configure { dependsOn("assembleRelease") }
    }
}
