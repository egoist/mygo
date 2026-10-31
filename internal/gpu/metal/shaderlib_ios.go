package metal

// The compiled library is macOS's, which iOS cannot load: the renderer
// compiles the shader's source there as it starts.
const shaderLibrarySum = ""

var shaderLibrary []byte
