package i18n

// AppLockMessages names the application lock, separate from remote credentials.
type AppLockMessages struct {
	Protection, ChangeAction, SystemBeforePassword                             string
	UpdatingDescription                                                        string
	DeviceCleanup, ProfileInUse, LargeRecoveryBackup                           string
	WrongPassword, Corrupt, MigrationPending, SystemMissing, DevicePassword    string
	Lock, Title, Description, ColdDescription, KeepRunning, RestoreAfterUnlock string
	Password, PasswordPlaceholder, UnlockPassword, UnlockSystem, Divider       string
	Working, Required, Mismatch, NewPassword, ConfirmPassword, CurrentPassword string
	SettingsTitle, SettingsDescription, DisabledDescription                    string
	SetPassword, ChangePassword, Disable, LockNow, LockNowDescription          string
	SystemTitle, SystemDescription, SystemDisabled, SystemUnavailable          string
	EnableSystem, DisableSystem, EnableTitle, ChangeTitle, DisableTitle        string
	EnableDescription, ChangeDescription, DisableDescription                   string
	EnrollTitle, EnrollDescription, RevokeTitle, RevokeDescription             string
	Save, Cancel, Error, Minimum, SystemCanceled, Updating                     string
}
