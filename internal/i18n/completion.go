package i18n

// CompletionMessages are the texts of command completion: the assembled list,
// its Settings group and the catalog's status.
type CompletionMessages struct {
	Title                 string
	Open                  string
	ModeOff               string
	ModeList              string
	ModeBoth              string
	KeysGroup             string
	KeysAccept            string
	KeysAcceptWord        string
	KeysSelect            string
	KeysInsert            string
	KeysInsertDescription string
	KeysClose             string
	KeysShell             string
	KeysShellDescription  string
	Group                 string
	CatalogRow            string
	CatalogDescriptionFmt string
	CatalogLoading        string
	CatalogOff            string
	Check                 string
	Updating              string
	UpToDate              string
	UpdatedFmt            string
	// UpdatedRepairedFmt is shown when an update needed repair or skipped
	// upstream files; the other UpdatedFmt stays the clean case.
	UpdatedRepairedFmt string
	// RepairSummaryFmt is the settings-row fragment for those nonzero counts.
	RepairSummaryFmt string
	// RecoveryStatsUnknown identifies legacy snapshots without import counters.
	RecoveryStatsUnknown string
	// CatalogProvenanceFmt is the settings row when the catalog commit is a
	// full hash: release, repo@short-commit, counts and origin.
	CatalogProvenanceFmt string
	// CatalogRepoFmt renders repository and short commit inside that row.
	CatalogRepoFmt string
	// CatalogOriginBuiltin and CatalogOriginInstalled name where the active
	// catalog came from.
	CatalogOriginBuiltin   string
	CatalogOriginInstalled string
	// AuditWarningsFmt reports soft findings of the content audit.
	AuditWarningsFmt string
	CheckFailedFmt   string
	CacheFailedFmt   string
	CheckedFmt       string
	NoDataDir        string
	// PrefixRow names the shared-prefix row of the history matches.
	PrefixRow string
	// The relative age of a history match, as its row shows it. TimeJustNow
	// covers a match used less than a minute ago.
	TimeJustNow    string
	TimeMinutesFmt string
	TimeHoursFmt   string
	TimeDaysFmt    string
	TimeMonthsFmt  string
	TimeYearsFmt   string
}
