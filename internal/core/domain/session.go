package domain

import "time"

type FlashKind string

const (
	FlashSuccess FlashKind = "success"
	FlashError   FlashKind = "error"
)

// Flash is a one-shot message shown on the next page rendered for the session.
type Flash struct {
	Kind    FlashKind
	Message string
}

// Session is the server-side state of a logged-in user. The raw token only
// exists in the browser cookie; the store keeps its hash.
type Session struct {
	TokenHash string
	UserID    int
	CSRFToken string
	CreatedAt time.Time
	ExpiresAt time.Time
	Flash     *Flash
}
