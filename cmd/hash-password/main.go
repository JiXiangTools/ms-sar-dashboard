package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	password := flag.String("password", "", "password to hash with bcrypt")
	flag.Parse()

	if strings.TrimSpace(*password) == "" {
		fmt.Fprintln(os.Stderr, "password is required")
		os.Exit(1)
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hash password: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(hashed))
}
