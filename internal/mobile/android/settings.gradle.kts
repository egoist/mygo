// MyGo's Android host: the `mygo` library (the bridge to Go, the views,
// the web view) and `app`, the shell app mygo build makes for each app.
pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}
rootProject.name = "mygo-android"
include(":mygo", ":app")
