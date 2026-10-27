package media

import (
	"crypto/rand"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// nameCodeLetters are the characters of the random code in a saved file name.
const nameCodeLetters = "abcdefghijklmnopqrstuvwxyz0123456789"

// nameCodeLength is how many characters the random code has.
const nameCodeLength = 6

// savedName matches a name OutputName made, capturing its number.
var savedName = regexp.MustCompile(`^(\d+)-snakemedia-`)

// OutputName is the name (without extension) of a file the app saves for the
// user: "<n>-snakemedia-<provider>-<6 random characters>". n is the file's
// place in its batch (or the next free number of the folder for a file saved
// by hand); the code keeps names from colliding.
func OutputName(n int, provider string) string {
	if n < 1 {
		n = 1
	}
	code := make([]byte, nameCodeLength)
	_, _ = rand.Read(code)
	for i, b := range code {
		code[i] = nameCodeLetters[int(b)%len(nameCodeLetters)]
	}
	return fmt.Sprintf("%d-snakemedia-%s-%s", n, provider, code)
}

// NextNumber is one more than the highest number of the files OutputName made
// in dir (1 for a folder without any).
func NextNumber(dir string) int {
	entries, _ := os.ReadDir(dir)
	highest := 0
	for _, e := range entries {
		if m := savedName.FindStringSubmatch(e.Name()); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > highest {
				highest = n
			}
		}
	}
	return highest + 1
}

// ProviderOf names the provider that made a stored file, for OutputName.
// Muse stores its results as "muse-…"; every other result comes from Flow.
func ProviderOf(localURL string) string {
	if strings.HasPrefix(strings.TrimPrefix(localURL, Route), "muse-") {
		return "muse"
	}
	return "flow"
}
