package sessionlog

import "fmt"

// Live elapsed time is not a stable paging key; unfinished rows form a final group.
const durationKey = `MAX(COALESCE(ended_at - started_at, 0), 0)`
const ongoingKey = `(ended_at IS NULL)`

func validateCursor(query Query) error {
	if query.Sort > SortDurationAsc {
		return ErrInvalidRecord
	}
	cursor := query.Cursor
	if cursor.ID == "" && cursor.StartedAt.IsZero() {
		return nil
	}
	if cursor.StartedAt.IsZero() || cursor.Sort != query.Sort {
		return ErrInvalidRecord
	}
	if err := validateKey("cursor id", cursor.ID); err != nil {
		return err
	}
	if query.Sort >= SortDurationDesc && (cursor.DurationMillis < 0 || cursor.Ongoing && cursor.DurationMillis != 0) {
		return ErrInvalidRecord
	}
	return nil
}

func queryOrder(order SortOrder) string {
	switch order {
	case SortStartedAsc:
		return `started_at ASC, id ASC`
	case SortDurationDesc:
		return ongoingKey + ` ASC, ` + durationKey + ` DESC, started_at DESC, id DESC`
	case SortDurationAsc:
		return ongoingKey + ` ASC, ` + durationKey + ` ASC, started_at DESC, id DESC`
	default:
		return `started_at DESC, id DESC`
	}
}

func cursorPredicate(query Query) (string, []any) {
	c := query.Cursor
	if c.ID == "" {
		return "", nil
	}
	at := c.StartedAt.UnixMilli()
	switch query.Sort {
	case SortStartedAsc:
		return `(started_at > ? OR (started_at = ? AND id > ?))`, []any{at, at, c.ID}
	case SortDurationDesc, SortDurationAsc:
		comparison := "<"
		if query.Sort == SortDurationAsc {
			comparison = ">"
		}
		clause := fmt.Sprintf(`(%s > ? OR (%s = ? AND (%s %s ? OR (%s = ? AND (started_at < ? OR (started_at = ? AND id < ?))))))`, ongoingKey, ongoingKey, durationKey, comparison, durationKey)
		group := boolInt(c.Ongoing)
		return clause, []any{group, group, c.DurationMillis, c.DurationMillis, at, at, c.ID}
	default:
		return `(started_at < ? OR (started_at = ? AND id < ?))`, []any{at, at, c.ID}
	}
}

func cursorFor(record Record, order SortOrder) Cursor {
	cursor := Cursor{StartedAt: record.StartedAt, ID: record.ID, Sort: order}
	if order >= SortDurationDesc {
		cursor.Ongoing = record.EndedAt.IsZero()
		if !cursor.Ongoing {
			cursor.DurationMillis = max(record.EndedAt.UnixMilli()-record.StartedAt.UnixMilli(), 0)
		}
	}
	return cursor
}
