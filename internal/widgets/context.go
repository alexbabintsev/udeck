package widgets

type Context struct {
	PageIndex   int
	TotalPages  int
	FolderDepth int
}

func (c Context) InFolder() bool {
	return c.FolderDepth > 0
}
