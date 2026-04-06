package dbsqlc

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func Timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{
		Time:  value,
		Valid: true,
	}
}

func NullableTimestamptz(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return Timestamptz(value.UTC())
}

func TimeValue(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func TimePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	timestamp := value.Time.UTC()
	return &timestamp
}

func RawMessagePtr(value []byte) *json.RawMessage {
	if value == nil {
		return nil
	}
	raw := json.RawMessage(append([]byte(nil), value...))
	return &raw
}
