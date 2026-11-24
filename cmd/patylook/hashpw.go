package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"patylook/internal/adapter/security"
	"patylook/internal/core/domain"
)

// hashPassword implements `patylook hash-password`: it prints a bcrypt hash that can be
// stored in user.password (for example to give an existing user a new password via SQL).
// It never touches the database.
func hashPassword(in *os.File, out io.Writer) error {
	password, err := readPassword(in, out)
	if err != nil {
		return err
	}
	if err := domain.ValidatePassword("password", password); err != nil {
		return err
	}
	hash, err := security.NewBcryptHasher().Hash(password)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, hash)
	return nil
}

func readPassword(in *os.File, out io.Writer) (string, error) {
	if term.IsTerminal(int(in.Fd())) {
		fmt.Fprint(os.Stderr, "Senha: ")
		b, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		fmt.Fprint(os.Stderr, "Confirme a senha: ")
		again, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		if string(b) != string(again) {
			return "", errors.New("as senhas não conferem")
		}
		return string(b), nil
	}
	// Non-interactive: read a single line from stdin.
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
