import com.android.build.api.artifact.SingleArtifact
import java.util.Properties

plugins {
    alias(libs.plugins.androidApplication)
}

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
        applicationId = "com.cornspies.music"
        minSdk = 27
        targetSdk = 37
        versionCode = 1
        versionName = "0.3.1"

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
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
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
        packageTask.configure { dependsOn("assembleRelease") }
    }
}
