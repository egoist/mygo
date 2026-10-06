package platform

// DocumentState is a document window's native presentation.
type DocumentState struct {
	Title, Path string
	Dirty       bool
}
