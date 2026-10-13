package statistics

import (
	"database/sql"
	"errors"
	"net/url"
	"strconv"
	"time"
)

var errUserScope = errors.New("cannot read another user's statistics")

type Filter struct {
	Provider Provider
	Username string
	Model    sql.Null[string]
	From     sql.Null[time.Time]
	To       sql.Null[time.Time]
	Limit    int
	Offset   int
}

func parseFilter(values url.Values, username string, provider Provider) (Filter, error) {
	filter := Filter{Provider: provider, Username: username, Limit: 100}
	for key, entries := range values {
		if len(entries) != 1 || entries[0] == "" {
			return Filter{}, errors.New("query parameters require one nonempty value")
		}
		value := entries[0]
		var err error
		switch key {
		case "user":
			if value != username {
				return Filter{}, errUserScope
			}
		case "model":
			filter.Model = sql.Null[string]{V: value, Valid: true}
		case "from":
			filter.From, err = parseTime(value)
		case "to":
			filter.To, err = parseTime(value)
		case "limit":
			filter.Limit, err = parseInteger(value, 1, 500)
		case "offset":
			filter.Offset, err = parseInteger(value, 0, 1_000_000)
		default:
			return Filter{}, errors.New("supported parameters: from, to, model, user, limit, offset")
		}
		if err != nil {
			return Filter{}, err
		}
	}
	return filter, filter.Validate()
}

func (f Filter) Validate() error {
	if _, err := f.Provider.wireValue(); err != nil {
		return err
	}
	if f.Username == "" || f.Limit < 1 || f.Limit > 500 || f.Offset < 0 || f.Offset > 1_000_000 {
		return errors.New("invalid statistics scope or pagination")
	}
	if f.Model.Valid && f.Model.V == "" {
		return errors.New("model must not be empty")
	}
	if f.From.Valid && f.From.V.IsZero() || f.To.Valid && f.To.V.IsZero() {
		return errors.New("invalid period")
	}
	if f.From.Valid && f.To.Valid && !f.From.V.Before(f.To.V) {
		return errors.New("from must be earlier than to")
	}
	return nil
}

func parseTime(value string) (sql.Null[time.Time], error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		parsed, err = time.Parse(time.DateOnly, value)
	}
	if err != nil {
		return sql.Null[time.Time]{}, errors.New("from and to must be YYYY-MM-DD or RFC3339 timestamps")
	}
	return sql.Null[time.Time]{V: parsed.UTC(), Valid: true}, nil
}

func parseInteger(value string, minimum, maximum int) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, errors.New("limit must be 1..500; offset must be 0..1000000")
	}
	return parsed, nil
}
