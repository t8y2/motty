package i18n

// CodeEditorMessages is the text of the code editor of the file window: its
// find and replace bar, the line jump and the status line.
type CodeEditorMessages struct {
	FindLabel, FindPlaceholder, ReplaceLabel, ReplacePlaceholder string
	FindPrevious, FindNext, FindClose                            string
	FindMatchCase, FindWholeWord, FindRegexp, FindToggleReplace  string
	FindReplaceOne, FindReplaceAll                               string
	// FindCountFmt is the match the selection is on and how many there are;
	// FindTotalFmt is the count when the selection is not on one, and
	// FindMoreFmt when the search stopped at its limit.
	FindCountFmt, FindTotalFmt, FindMoreFmt string
	FindNone, FindInvalid                   string
	FindReplacedFmt                         string
	GoToLineLabel, GoToLinePlaceholderFmt   string
	// PositionFmt is the caret's line and column; SelectedFmt follows it when
	// text is selected.
	PositionFmt, SelectedFmt string
	IndentTabs, IndentSpaces string
	WrapOn, WrapOff          string
}

var englishCodeEditor = CodeEditorMessages{
	FindLabel: "Find in file", FindPlaceholder: "Find", ReplaceLabel: "Replace with", ReplacePlaceholder: "Replace",
	FindPrevious: "Previous match", FindNext: "Next match", FindClose: "Close find",
	FindMatchCase: "Match case", FindWholeWord: "Whole word", FindRegexp: "Regular expression", FindToggleReplace: "Show replace",
	FindReplaceOne: "Replace", FindReplaceAll: "All",
	FindCountFmt: "%d of %d", FindTotalFmt: "%d found", FindMoreFmt: "%d+ found",
	FindNone: "No results", FindInvalid: "Invalid expression",
	FindReplacedFmt: "Replaced %d",
	GoToLineLabel:   "Go to line", GoToLinePlaceholderFmt: "Line 1–%d",
	PositionFmt: "Ln %d, Col %d", SelectedFmt: "%d selected",
	IndentTabs: "Tabs", IndentSpaces: "Spaces: %d",
	WrapOn: "Wrap", WrapOff: "No wrap",
}

var chineseCodeEditor = CodeEditorMessages{
	FindLabel: "在文件中查找", FindPlaceholder: "查找", ReplaceLabel: "替换为", ReplacePlaceholder: "替换",
	FindPrevious: "上一个匹配", FindNext: "下一个匹配", FindClose: "关闭查找",
	FindMatchCase: "区分大小写", FindWholeWord: "全字匹配", FindRegexp: "正则表达式", FindToggleReplace: "显示替换",
	FindReplaceOne: "替换", FindReplaceAll: "全部",
	FindCountFmt: "%d / %d 项", FindTotalFmt: "共 %d 项", FindMoreFmt: "%d+ 项",
	FindNone: "无结果", FindInvalid: "表达式无效",
	FindReplacedFmt: "已替换 %d 处",
	GoToLineLabel:   "跳转到行", GoToLinePlaceholderFmt: "行号 1–%d",
	PositionFmt: "行 %d，列 %d", SelectedFmt: "已选 %d",
	IndentTabs: "制表符", IndentSpaces: "空格：%d",
	WrapOn: "自动换行", WrapOff: "不换行",
}
