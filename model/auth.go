package model

type AuthHandler struct {
	Token string
}

func NewAuthHandler(token string) *AuthHandler {
	return &AuthHandler{Token: token}
}
