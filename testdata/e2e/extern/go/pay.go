package ext

import (
	"context"
	"errors"

	"vibeapp/model"
	vibe "vibeapp/runtime"
)

// ChargeCard pretends to charge a card: the token "declined" fails like
// fail conflict, "boom" with an internal error.
func ChargeCard(ctx context.Context, amount vibe.Money, token string) (string, error) {
	switch token {
	case "declined":
		return "", vibe.Fail(vibe.Conflict, "card declined")
	case "boom":
		return "", errors.New("provider unreachable")
	}
	return "charged " + amount.String(), nil
}

// Describe describes a book.
func Describe(ctx context.Context, b *model.Book) (string, error) {
	return b.Title + " costs " + b.Price.String(), nil
}
