package database

import "golang.org/x/crypto/bcrypt"

const passwordBcryptCost = 12

func comparePasswordHash(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordBcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
