package mock

import "math/rand"

var chars = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

func generateString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}
