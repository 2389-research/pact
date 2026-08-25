// ABOUTME: Provides cancellable deterministic work loops for index projection and proof code.
// ABOUTME: Polls before secondary allocations and while merging bounded sorted rows.
package index

import (
	"context"
	"fmt"
	"sort"
)

const indexSortChunkSize = 256

var afterIndexWorkPoll = func() {}

func pollIndexContext(ctx context.Context, work int) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if work%64 == 0 {
		afterIndexWorkPoll()
		return ctx.Err()
	}
	return nil
}

func sortIndexRowsContext[Row any](ctx context.Context, rows []Row, less func(Row, Row) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for start := 0; start < len(rows); start += indexSortChunkSize {
		if err := pollIndexContext(ctx, start); err != nil {
			return err
		}
		end := min(start+indexSortChunkSize, len(rows))
		sort.Slice(rows[start:end], func(left, right int) bool {
			return less(rows[start+left], rows[start+right])
		})
	}
	if len(rows) <= indexSortChunkSize {
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	buffer := make([]Row, len(rows))
	source, destination := rows, buffer
	sourceIsRows := true
	for width := indexSortChunkSize; width < len(rows); width *= 2 {
		for start := 0; start < len(rows); start += 2 * width {
			middle := min(start+width, len(rows))
			end := min(start+2*width, len(rows))
			left, right := start, middle
			for output := start; output < end; output++ {
				if err := pollIndexContext(ctx, output-start); err != nil {
					return err
				}
				if right == end || left < middle && !less(source[right], source[left]) {
					destination[output] = source[left]
					left++
				} else {
					destination[output] = source[right]
					right++
				}
			}
		}
		source, destination = destination, source
		sourceIsRows = !sourceIsRows
	}
	if sourceIsRows {
		return ctx.Err()
	}
	for index := range source {
		if err := pollIndexContext(ctx, index); err != nil {
			return err
		}
		rows[index] = source[index]
	}
	return ctx.Err()
}
