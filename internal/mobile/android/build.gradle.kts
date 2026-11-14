plugins {
    id("com.android.application") version "8.12.0" apply false
    id("com.android.library") version "8.12.0" apply false
    id("org.jetbrains.kotlin.android") version "2.1.20" apply false
}

// The native modules' libraries: their build sets only their namespace and
// dependencies, and this the rest, as the host's own. They build here, as
// a module in Go's module cache is read-only.
val nativeModules = providers.gradleProperty("mygoNativeModules").orNull?.split(',')?.filter { it.isNotEmpty() }?.associate {
    val parts = it.split('|')
    parts[0] to parts.getOrElse(2) { "" }.split(' ').filter { d -> d.isNotEmpty() }
} ?: emptyMap()

subprojects {
    val uses = nativeModules[name] ?: return@subprojects
    layout.buildDirectory.set(rootProject.layout.buildDirectory.dir("native/$name"))
    plugins.withId("com.android.library") {
        extensions.configure<com.android.build.gradle.LibraryExtension> {
            compileSdk = 36
            defaultConfig {
                minSdk = (providers.gradleProperty("mygoMinSdk").orNull ?: "26").toInt()
            }
            compileOptions {
                sourceCompatibility = JavaVersion.VERSION_17
                targetCompatibility = JavaVersion.VERSION_17
            }
        }
        dependencies {
            add("implementation", project(":mygo"))
            uses.forEach { add("implementation", project(":$it")) }
        }
    }
    plugins.withId("org.jetbrains.kotlin.android") {
        extensions.configure<org.jetbrains.kotlin.gradle.dsl.KotlinAndroidProjectExtension> { jvmToolchain(17) }
    }
}
