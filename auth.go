package socks4

// AuthenticationFunc Authentication interface is implemented
type AuthenticationFunc func(username string) bool

// Auth authentication processing
func (f AuthenticationFunc) Auth(username string) bool {
	return f(username)
}

// Authentication proxy authentication
type Authentication interface {
	Auth(username string) bool
}

// UserAuth basic authentication
func UserAuth(username string) Authentication {
	return AuthenticationFunc(func(u string) bool {
		return username == u
	})
}
