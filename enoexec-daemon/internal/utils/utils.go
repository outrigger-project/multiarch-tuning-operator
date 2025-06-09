package utils

import "os"

func Should(err error) {
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
	}
}
