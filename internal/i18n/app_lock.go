package i18n

// AppLockMessages holds application lock labels and errors.
type AppLockMessages struct {
	Protection, ChangeAction, SystemBeforePassword                             string
	WrongPassword, Corrupt, DevicePassword                                     string
	Lock, Title                                                                string
	KeepRunning, RestoreAfterUnlock                                            string
	UnlockToReview                                                             string
	Password, PasswordPlaceholder, UnlockPassword, UnlockSystem, Divider       string
	Working, Required, Mismatch, NewPassword, ConfirmPassword, CurrentPassword string
	SettingsTitle, SettingsDescription, DisabledDescription, ChangePassword    string
	SystemTitle, SystemDescription, SystemDisabled, SystemUnavailable          string
	SystemForeground, SystemPrompt, PasswordTooLong                            string
	EnableTitle, ChangeTitle, DisableTitle                                     string
	EnableDescription, ChangeDescription, DisableDescription                   string
	EnrollTitle, EnrollDescription                                             string
	Save, Cancel, Error, Minimum                                               string
}
