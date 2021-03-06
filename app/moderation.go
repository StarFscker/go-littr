package app

import (
	pub "github.com/go-ap/activitypub"
	"github.com/go-ap/errors"
	"html/template"
	"strings"
	"time"
)

var ModerationActivityTypes = pub.ActivityVocabularyTypes{pub.BlockType, pub.IgnoreType, pub.FlagType}

// ModerationRequests
type ModerationRequests []ModerationRequest

type ModerationRequest struct {
	Hash        Hash              `json:"hash"`
	Icon        template.HTML     `json:"-"`
	Typ         RenderType        `json:"type,omitempty"`
	SubmittedAt time.Time         `json:"-"`
	Data        string            `json:"-"`
	SubmittedBy *Account          `json:"by,omitempty"`
	Object      Renderable        `json:"-"`
	Metadata    *ActivityMetadata `json:"-"`
	pub         pub.Item          `json:"-"`
	Flags       FlagBits          `json:"flags,omitempty"`
}

// Type
func (m *ModerationRequest) Type() RenderType {
	return m.Typ
}

// IsValid returns if the current follow request has a hash with length greater than 0
func (m *ModerationRequest) IsValid() bool {
	return m != nil && len(m.Hash) > 0
}

// Date
func (m ModerationRequest) Date() time.Time {
	return m.SubmittedAt
}

// Private
func (m *ModerationRequest) Private() bool {
	return m.Flags&FlagsPrivate == FlagsPrivate
}

// Deleted
func (m *ModerationRequest) Deleted() bool {
	return m.Flags&FlagsDeleted == FlagsDeleted
}

func (m *ModerationRequest) FromActivityPub(it pub.Item) error {
	if m == nil {
		return nil
	}
	if it == nil {
		return errors.Newf("nil item received")
	}
	m.pub = it
	if it.IsLink() {
		iri := it.GetLink()
		m.Hash.FromActivityPub(iri)
		m.Metadata = &ActivityMetadata{
			ID: iri.String(),
		}
		return nil
	}
	return pub.OnActivity(it, func(a *pub.Activity) error {
		m.Hash.FromActivityPub(a)
		wer := new(Account)

		switch a.Type {
		case pub.BlockType:
			m.Typ = ModerationBlock
		case pub.IgnoreType:
			m.Typ = ModerationIgnored
		case pub.FlagType:
			m.Typ = ModerationReported
		}

		m.Icon = icon(strings.ToLower(string(a.Type)))
		wer.FromActivityPub(a.Actor)
		m.SubmittedBy = wer
		if strings.Contains(a.Object.GetLink().String(), "actors") {
			ob := new(Account)
			if err := ob.FromActivityPub(a.Object); err == nil {
				m.Object = ob
			}
		}
		if strings.Contains(a.Object.GetLink().String(), "objects") {
			ob := new(Item)
			if err := ob.FromActivityPub(a.Object); err == nil {
				m.Object = ob
			}
		}
		reason := new(Item)
		pub.OnObject(a, func(o *pub.Object) error {
			return FromArticle(reason, o)
		})
		if len(reason.Data) > 0 {
			m.Data = reason.Data
		}
		m.SubmittedAt = a.Published
		m.Metadata = &ActivityMetadata{
			ID: string(a.ID),
		}

		return nil
	})
}
