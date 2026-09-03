package burner

import (
	"fmt"
	"math/rand"

	"github.com/google/uuid"
)

var testWords = []string{"quick", "lazy", "happy", "brave", "swift", "smart", "bold", "calm", "dark", "damp", "early", "fast", "glad", "high", "kind", "light", "neat", "open", "proud", "quiet", "sharp", "tiny", "vast", "warm", "wide"}

func randomJobName() string {
	w1 := testWords[rand.Intn(len(testWords))]
	w2 := testWords[rand.Intn(len(testWords))]
	w3 := testWords[rand.Intn(len(testWords))]
	return fmt.Sprintf("%s-%s-%s", w1, w2, w3)
}

func randomUUID() string {
	return uuid.New().String()
}
