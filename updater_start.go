//go:build mygo_updates && !js

package mygo

// mygo build builds an app with updates with the tag mygo_updates, which
// has its start judge the first start of an update (judgeUpdatedStart):
// other apps do not carry it.
func init() { updatedStart = judgeUpdatedStart }
