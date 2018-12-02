package app

import (
	"path"
	"strings"

	ap "github.com/mariusor/littr.go/app/activitypub"

	"github.com/buger/jsonparser"
	"github.com/juju/errors"

	as "github.com/mariusor/activitypub.go/activitystreams"
)

type Converter interface {
	FromActivityPub(ob as.Item) error
}

func (h *Hash) FromActivityPub(it as.Item) error {
	*h = getHashFromAP(it.GetLink())
	return nil
}

func (a *Account) FromActivityPub(it as.Item) error {
	if it == nil {
		return errors.New("nil item received")
	}
	if it.IsLink() {
		iri := it.GetLink()
		a.Hash.FromActivityPub(iri)
		a.Metadata = &AccountMetadata{
			ID: iri.String(),
		}
		return nil
	}
	switch it.GetType() {
	case as.CreateType:
		fallthrough
	case as.UpdateType:
		if act, ok := it.(*ap.Activity); ok {
			return a.FromActivityPub(act.Actor)
		}
	case as.PersonType:
		if p, ok := it.(*ap.Person); ok {
			a.Score = p.Score
			a.Metadata = &AccountMetadata{
				Key: &SSHKey{
					ID:     "",
					Public: []byte(p.PublicKey.PublicKeyPem),
				},
			}
			name := jsonUnescape(as.NaturalLanguageValue(p.Name).First())
			a.Hash = getHashFromAP(p)
			a.Handle = name
			a.Email = ""
			a.Flags = FlagsNone
			if len(p.ID) > 0 {
				iri := p.GetLink()
				a.Metadata = &AccountMetadata{
					ID:  iri.String(),
					URL: p.URL.GetLink().String(),
				}
			}
		}
	default:
		return errors.New("invalid object type")
	}

	return nil
}

func (i *Item) FromActivityPub(it as.Item) error {
	if it == nil {
		return errors.New("nil item received")
	}
	if it.IsLink() {
		i.Hash.FromActivityPub(it.GetLink())
		return nil
	}
	switch it.GetType() {
	case as.CreateType:
		fallthrough
	case as.UpdateType:
		fallthrough
	case as.ActivityType:
		if act, ok := it.(*ap.Activity); ok {
			err := i.FromActivityPub(act.Object)
			i.SubmittedBy.FromActivityPub(act.Actor)
			return err
		}
		if act, ok := it.(ap.Activity); ok {
			err := i.FromActivityPub(act.Object)
			i.SubmittedBy.FromActivityPub(act.Actor)
			return err
		}
	case as.ArticleType:
		fallthrough
	case as.NoteType:
		fallthrough
	case as.DocumentType:
		fallthrough
	case as.PageType:
		if a, ok := it.(ap.Article); ok {
			i.Score = a.Score
			i.Hash = getHashFromAP(a)
			title := jsonUnescape(as.NaturalLanguageValue(a.Name).First())
			content := jsonUnescape(as.NaturalLanguageValue(a.Content).First())

			i.Hash = getHashFromAP(a)
			i.Title = title
			i.MimeType = string(a.MediaType)
			if a.Type == as.PageType {
				i.Data = string(a.URL.GetLink())
				i.MimeType = MimeTypeURL
			} else {
				i.Data = content
			}
			i.SubmittedAt = a.Published

			if a.AttributedTo != nil {
				if a.AttributedTo.IsObject() {
					auth := Account{}
					auth.FromActivityPub(a.AttributedTo)
					i.SubmittedBy = &auth
				} else {
					i.SubmittedBy = &Account{
						Handle: getAccountHandle(a.AttributedTo.GetLink()),
					}
				}
			}

			if a.InReplyTo != nil {
				par := Item{}
				par.FromActivityPub(a.InReplyTo)
				i.Parent = &par
			}
			if a.Context != nil {
				op := Item{}
				op.FromActivityPub(a.Context)
				i.OP = &op
			}
			if a.Tag != nil && len(a.Tag) > 0 {
				i.Metadata = &ItemMetadata{}
				i.Metadata.Tags = make(TagCollection, 0)
				i.Metadata.Mentions = make(TagCollection, 0)

				tags := TagCollection{}
				tags.FromActivityPub(a.Tag)
				for _, t := range tags {
					if t.Name[0] == '#' {
						i.Metadata.Tags = append(i.Metadata.Tags, t)
					} else {
						i.Metadata.Mentions = append(i.Metadata.Mentions, t)
					}
				}
			}
		}
	default:
		return errors.New("invalid object type")
	}

	return nil
}

func (v *Vote) FromActivityPub(it as.Item) error {
	if it == nil {
		return errors.New("nil item received")
	}
	if it.IsLink() {
		return errors.New("unable to load from IRI")
	}
	switch it.GetType() {
	case as.LikeType:
		fallthrough
	case as.DislikeType:
		if act, ok := it.(ap.Activity); ok {
			on := Item{}
			on.FromActivityPub(act.Object)
			v.Item = &on

			er := Account{}
			er.FromActivityPub(act.Actor)
			v.SubmittedBy = &er

			v.SubmittedAt = act.Published
			v.UpdatedAt = act.Updated
			if act.Type == as.LikeType {
				v.Weight = 1
			}
			if act.Type == as.DislikeType {
				v.Weight = -1
			}
		}
		if act, ok := it.(*ap.Activity); ok {
			on := Item{}
			on.FromActivityPub(act.Object)
			v.Item = &on

			er := Account{}
			er.FromActivityPub(act.Actor)
			v.SubmittedBy = &er

			v.SubmittedAt = act.Published
			v.UpdatedAt = act.Updated
			if act.Type == as.LikeType {
				v.Weight = 1
			}
			if act.Type == as.DislikeType {
				v.Weight = -1
			}
		}
	}

	return nil
}

func getHashFromAP(obj as.Item) Hash {
	s := strings.Split(obj.GetLink().String(), "/")
	var hash string
	if s[len(s)-1] == "object" {
		hash = s[len(s)-2]
	} else {
		hash = s[len(s)-1]
	}
	return Hash(path.Base(hash))
}

func getAccountHandle(o as.Item) string {
	if o == nil {
		return ""
	}
	i := o.(as.IRI)
	s := strings.Split(string(i), "/")
	return s[len(s)-1]
}

func jsonUnescape(s string) string {
	var out []byte
	var err error
	if out, err = jsonparser.Unescape([]byte(s), nil); err != nil {
		Logger.Error(err.Error())
		return s
	}
	return string(out)
}

func (i *TagCollection) FromActivityPub(it as.ItemCollection) error {
	if it == nil || len(it) == 0 {
		return errors.New("empty collection")
	}
	for _, t := range it {
		if m, ok := t.(*as.Mention); ok {
			u := string(*t.GetID())
			// we have a link
			lt := Tag{
				URL:  u,
				Name: m.Name.First(),
			}
			*i = append(*i, lt)
		}
		if ob, ok := t.(*as.Object); ok {
			u := string(*t.GetID())
			// we have a link
			lt := Tag{
				URL:  u,
				Name: ob.Name.First(),
			}
			*i = append(*i, lt)
		}
	}
	return nil
}
