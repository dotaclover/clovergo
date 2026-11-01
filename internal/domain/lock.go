package domain

// AppLock is the stored global password: a salted PBKDF2 hash, never the
// password itself. No row means the app is not locked.
type AppLock struct {
	PasswordHash string
	Salt         string
	Iterations   int
	UpdatedAt    string
}
