// Package mongorepo implements port.UserRepository on MongoDB using the
// official Go driver.
package mongorepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"user-management/internal/domain"
	"user-management/internal/port"
)

// CollectionName is the MongoDB collection that stores users.
const CollectionName = "users"

// userDocument is the persistence model. It is intentionally separate from
// domain.User so that BSON tags and ObjectID types never leak into the domain.
type userDocument struct {
	ID           bson.ObjectID `bson:"_id"`
	Name         string        `bson:"name"`
	Email        string        `bson:"email"`
	PasswordHash string        `bson:"password_hash"`
	CreatedAt    time.Time     `bson:"created_at"`
}

func (d *userDocument) toDomain() *domain.User {
	return &domain.User{
		ID:           d.ID.Hex(),
		Name:         d.Name,
		Email:        d.Email,
		PasswordHash: d.PasswordHash,
		CreatedAt:    d.CreatedAt.UTC(),
	}
}

// UserRepository is a MongoDB-backed port.UserRepository.
type UserRepository struct {
	coll *mongo.Collection
}

var _ port.UserRepository = (*UserRepository)(nil)

// NewUserRepository returns a repository using db.users.
func NewUserRepository(db *mongo.Database) *UserRepository {
	return &UserRepository{coll: db.Collection(CollectionName)}
}

// EnsureIndexes creates the unique index on email. It is idempotent and
// should be called once at startup.
func (r *UserRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("uniq_email"),
	})
	if err != nil {
		return fmt.Errorf("create email index: %w", err)
	}
	return nil
}

// Create inserts user and sets user.ID.
func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
	doc := userDocument{
		ID:           bson.NewObjectID(),
		Name:         user.Name,
		Email:        user.Email,
		PasswordHash: user.PasswordHash,
		CreatedAt:    user.CreatedAt,
	}
	if _, err := r.coll.InsertOne(ctx, doc); err != nil {
		return mapWriteError(err)
	}
	user.ID = doc.ID.Hex()
	return nil
}

// GetByID finds a user by hex ObjectID.
func (r *UserRepository) GetByID(ctx context.Context, id string) (*domain.User, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, domain.ErrUserNotFound
	}
	return r.findOne(ctx, bson.D{{Key: "_id", Value: oid}})
}

// GetByEmail finds a user by (normalized) email.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	return r.findOne(ctx, bson.D{{Key: "email", Value: email}})
}

func (r *UserRepository) findOne(ctx context.Context, filter bson.D) (*domain.User, error) {
	var doc userDocument
	if err := r.coll.FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("find user: %w", err)
	}
	return doc.toDomain(), nil
}

// List returns users ordered by creation (ObjectIDs are time-ordered).
func (r *UserRepository) List(ctx context.Context, offset, limit int64) ([]domain.User, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: 1}}).
		SetSkip(offset).
		SetLimit(limit)
	cur, err := r.coll.Find(ctx, bson.D{}, opts)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	var docs []userDocument
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode users: %w", err)
	}
	users := make([]domain.User, 0, len(docs))
	for i := range docs {
		users = append(users, *docs[i].toDomain())
	}
	return users, nil
}

// Update applies changes atomically and returns the updated user.
func (r *UserRepository) Update(ctx context.Context, id string, changes port.UserChanges) (*domain.User, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, domain.ErrUserNotFound
	}
	set := bson.D{}
	if changes.Name != nil {
		set = append(set, bson.E{Key: "name", Value: *changes.Name})
	}
	if changes.Email != nil {
		set = append(set, bson.E{Key: "email", Value: *changes.Email})
	}
	if len(set) == 0 {
		return r.GetByID(ctx, id)
	}

	var doc userDocument
	err = r.coll.FindOneAndUpdate(ctx,
		bson.D{{Key: "_id", Value: oid}},
		bson.D{{Key: "$set", Value: set}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrUserNotFound
		}
		return nil, mapWriteError(err)
	}
	return doc.toDomain(), nil
}

// Delete removes a user by ID.
func (r *UserRepository) Delete(ctx context.Context, id string) error {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return domain.ErrUserNotFound
	}
	res, err := r.coll.DeleteOne(ctx, bson.D{{Key: "_id", Value: oid}})
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if res.DeletedCount == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// Count returns the exact number of users.
func (r *UserRepository) Count(ctx context.Context) (int64, error) {
	n, err := r.coll.CountDocuments(ctx, bson.D{})
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

func mapWriteError(err error) error {
	if mongo.IsDuplicateKeyError(err) {
		return domain.ErrEmailTaken
	}
	return fmt.Errorf("write user: %w", err)
}
