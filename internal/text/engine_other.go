//go:build !(windows && (amd64 || arm64)) && !(darwin && !ios) && !(linux && !android && (amd64 || arm64)) && !js

package text

func newEngine() engine { return &stubEngine{} }
