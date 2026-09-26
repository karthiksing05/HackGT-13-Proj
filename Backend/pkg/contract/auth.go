package contract

// Empty is the {} body of endpoints that answer "ok" with nothing else.
var Empty = struct{}{}

type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    Time   `json:"expires_at"`
}

type AuthResponse struct {
	User   User   `json:"user"`
	Tokens Tokens `json:"tokens"`
}

type SignupRequest struct {
	Name        string  `json:"name"`
	Email       string  `json:"email"`
	Password    string  `json:"password"`
	Username    *string `json:"username,omitempty"`
	DateOfBirth *Time   `json:"date_of_birth,omitempty"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// UserPatch is PATCH /me: only the fields present change.
type UserPatch struct {
	Name        *string         `json:"name,omitempty"`
	Username    *string         `json:"username,omitempty"`
	DateOfBirth *Time           `json:"date_of_birth,omitempty"`
	Status      *PresenceStatus `json:"status,omitempty"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type RefreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    Time   `json:"expires_at"`
}

// LogoutRequest carries the session to revoke; the app may send null.
type LogoutRequest struct {
	RefreshToken *string `json:"refresh_token"`
}

// EmailRequest is POST /auth/password/forgot and …/resend.
type EmailRequest struct {
	Email string `json:"email"`
}

// ForgotResponse is {} in production and {"code": "123456"} with DEV_RESET_CODES=1.
type ForgotResponse struct {
	Code *string `json:"code,omitempty"`
}

type VerifyResetRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type ResetTokenResponse struct {
	ResetToken string `json:"reset_token"`
}

type ResetPasswordRequest struct {
	ResetToken  string `json:"reset_token"`
	NewPassword string `json:"new_password"`
}

// AvatarPatch is PATCH /me/avatar.
type AvatarPatch struct {
	Color AvatarColor `json:"color"`
}

// DeviceRegistration is POST /me/devices.
type DeviceRegistration struct {
	PushToken string `json:"push_token"`
	Platform  string `json:"platform"`
}

// URLResponse is the {url} shape of photo upload, hosted pages and invites.
type URLResponse struct {
	URL string `json:"url"`
}
