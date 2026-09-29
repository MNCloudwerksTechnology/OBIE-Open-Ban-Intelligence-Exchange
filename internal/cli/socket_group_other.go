//go:build !unix

package cli

// socketGroup returns "": the owner of a file cannot be told here.
func socketGroup(string) string { return "" }
