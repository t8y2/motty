package workbench

// MinimumWindowWidth and MinimumWindowHeight keep the workbench's navigation,
// titlebar controls and main content usable, in logical (DPI-independent) pixels.
// At the minimum height the sidebar shows its header at full spacing and every
// row down to Settings; a new row must raise the minimum rather than compress
// the header. Viva reduces a minimum only when the display's available work area
// is smaller.
const (
	MinimumWindowWidth  = 960
	MinimumWindowHeight = 620

	remoteToolsMinWidth  = 960
	remoteToolsMinHeight = 600
	sftpWindowMinWidth   = 900
	sftpWindowMinHeight  = 560
	fileWindowMinWidth   = 640
	fileWindowMinHeight  = 420
)
