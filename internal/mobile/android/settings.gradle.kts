// MyGo's Android host: the `mygo` library (the bridge to Go, the views,
// the web view) and `app`, the shell app mygo build makes for each app.
pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
// The dependencies' repositories are these; a project's own (an init script
// on the user's or CI machine adds some) are ignored rather than failing the
// build.
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.PREFER_SETTINGS)
    repositories {
        google()
        mavenCentral()
    }
}
rootProject.name = "mygo-android"
include(":mygo", ":app")
