package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	EventUserSignedUp       = "user.signed_up"
	EventProfileFollowed    = "profile.followed"
	EventProfileUnfollowed  = "profile.unfollowed"
	EventArticleCreated     = "article.created"
	EventArticleDeleted     = "article.deleted"
	EventArticleFavorited   = "article.favorited"
	EventArticleUnfavorited = "article.unfavorited"
	EventArticleTagFiltered = "article.filtered_by_tag"
	EventCommentCreated     = "comment.created"
	EventCommentDeleted     = "comment.deleted"
	EventTagsListed         = "tags.listed"
)

var (
	productEvents metric.Int64Counter
	articleTags   metric.Int64Histogram
)

func InitializeMetrics() error {
	meter := otel.Meter(Scope)
	var err error
	productEvents, err = meter.Int64Counter(
		"realworld.product.events",
		metric.WithDescription("Successful product milestones"),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		return fmt.Errorf("create product event counter: %w", err)
	}
	articleTags, err = meter.Int64Histogram(
		"realworld.article.tag_count",
		metric.WithDescription("Number of tags on a newly created article"),
		metric.WithUnit("{tag}"),
		metric.WithExplicitBucketBoundaries(0, 1, 2, 3, 5, 10),
	)
	if err != nil {
		return fmt.Errorf("create article tag histogram: %w", err)
	}
	return nil
}

func RecordProductEvent(ctx context.Context, name string) {
	if productEvents != nil {
		productEvents.Add(ctx, 1, metric.WithAttributes(attribute.String("event.name", name)))
	}
}

func RecordArticleTagCount(ctx context.Context, count int) {
	if articleTags != nil {
		articleTags.Record(ctx, int64(count))
	}
}
