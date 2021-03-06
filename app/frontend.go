package app

import (
	"context"
	xerrors "errors"
	"fmt"
	pub "github.com/go-ap/activitypub"
	"github.com/go-ap/errors"
	"github.com/go-chi/chi"
	"github.com/gorilla/csrf"
	"github.com/gorilla/sessions"
	"github.com/mariusor/littr.go/internal/config"
	"github.com/mariusor/littr.go/internal/log"
	"golang.org/x/oauth2"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	sessionName = "_s"
	csrfName    = "_c"
	cookieBackend = "cookie"
	fsBackend = "fs"
)

type handler struct {
	conf    appConfig
	v       *view
	logger  log.Logger
	storage *repository
}

var defaultAccount = AnonymousAccount

type appConfig struct {
	config.Configuration
	BaseURL         string
	SessionKeys     [][]byte
	SessionsBackend string
	Logger          log.Logger
}

var defaultLogFn = func(string, ...interface{}) {}
var defaultCtxLogFn = func(c log.Ctx) LogFn { return defaultLogFn }

func Init(c appConfig) (*handler, error) {
	var err error

	h := new(handler)

	infoFn := defaultCtxLogFn
	errFn := defaultCtxLogFn

	if c.Logger != nil {
		h.logger = c.Logger
		infoFn = func(ctx log.Ctx) LogFn {
			return h.logger.WithContext(ctx).Infof
		}
		errFn = func(ctx log.Ctx) LogFn {
			return h.logger.WithContext(ctx).Errorf
		}
	}

	if c.SessionsBackend = os.Getenv("SESSIONS_BACKEND"); c.SessionsBackend == "" {
		c.SessionsBackend = "cookie"
	}
	c.SessionsBackend = strings.ToLower(c.SessionsBackend)
	c.SessionKeys = loadEnvSessionKeys()
	h.v, _ = ViewInit(c, infoFn, errFn)
	if h.v.s == nil {
		h.conf.SessionsEnabled = false
	}
	h.conf = c

	h.storage = ActivityPubService(c)
	key := os.Getenv("OAUTH2_KEY")
	pw := os.Getenv("OAUTH2_SECRET")
	if len(key) > 0 {
		oIRI := actors.IRI(pub.IRI(h.storage.BaseURL)).AddPath(key)
		oauth, err := h.storage.fedbox.Actor(oIRI)
		if err == nil {
			h.storage.app = new(Account)
			h.storage.app.FromActivityPub(oauth)
			config := GetOauth2Config("fedbox", h.conf.BaseURL)

			handle := h.storage.app.Handle
			tok, err := config.PasswordCredentialsToken(context.Background(), handle, pw)
			if err != nil {
				return h, err
			}
			if tok == nil {
				return h, err
			}
			h.storage.app.Metadata.OAuth.Provider = "fedbox"
			h.storage.app.Metadata.OAuth.Token = tok.AccessToken
			h.storage.app.Metadata.OAuth.TokenType = tok.TokenType
			h.storage.app.Metadata.OAuth.RefreshToken = tok.RefreshToken
		}
	}

	return h, err
}

func initCookieSession(h string, secure bool, k ...[]byte) (sessions.Store, error) {
	ss := sessions.NewCookieStore(k...)
	ss.Options.Domain = h
	ss.Options.Path = "/"
	ss.Options.HttpOnly = true
	ss.Options.Secure = secure
	return ss, nil
}

func (v view)initFileSession(h string, secure bool, k ...[]byte) (sessions.Store, error) {
	sessDir := fmt.Sprintf("%s/%s", os.TempDir(), h)
	if _, err := os.Stat(sessDir); err != nil {
		if os.IsNotExist(err) {
			if err := os.Mkdir(sessDir, 0700); err != nil {
				return nil, err
			}
		} else {
			v.errFn(nil)("Invalid path %s for saving sessions: %s", sessDir, err)
			return nil, err
		}
	}
	ss := sessions.NewFilesystemStore(sessDir, k...)
	ss.Options.Domain = h
	ss.Options.Path = "/"
	ss.Options.HttpOnly = true
	ss.Options.Secure = secure
	return ss, nil
}

type headerEl struct {
	IsCurrent bool
	Icon      string
	Auth      bool
	Name      string
	URL       string
}

func loggedAccount(r *http.Request) *Account {
	if acct := ContextAccount(r.Context()); acct != nil {
		return acct
	}
	return &defaultAccount
}

// HandleCallback serves /auth/{provider}/callback request
func (h *handler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	provider := chi.URLParam(r, "provider")
	providerErr := q["error"]
	if providerErr != nil {
		errDescriptions := q["error_description"]
		var errs = make([]error, 1)
		errs[0] = errors.Errorf("Error for provider %q:\n", provider)
		for _, errDesc := range errDescriptions {
			errs = append(errs, errors.Errorf(errDesc))
		}
		h.v.HandleErrors(w, r, errs...)
		return
	}
	code := q.Get("code")
	state := q.Get("state")
	if len(code) == 0 {
		h.v.HandleErrors(w, r, errors.Forbiddenf("%s error: Empty authentication token", provider))
		return
	}

	conf := GetOauth2Config(provider, h.conf.BaseURL)
	tok, err := conf.Exchange(r.Context(), code)
	if err != nil {
		h.logger.Errorf("%s", err)
		h.v.HandleErrors(w, r, err)
		return
	}

	s, _ := h.v.s.get(r)
	account := loadCurrentAccountFromSession(s, h.logger)
	if account.Metadata == nil {
		account.Metadata = &AccountMetadata{}
	}
	account.Metadata.OAuth = OAuth{
		State:        state,
		Code:         code,
		Provider:     provider,
		Token:        tok.AccessToken,
		TokenType:    tok.TokenType,
		RefreshToken: tok.RefreshToken,
		Expiry:       tok.Expiry,
	}

	s.Values[SessionUserKey] = account
	if strings.ToLower(provider) != "local" {
		h.v.addFlashMessage(Success, r, fmt.Sprintf("Login successful with %s", provider))
	} else {
		h.v.addFlashMessage(Success, r, "Login successful")
	}
	h.v.Redirect(w, r, "/", http.StatusFound)
}

func GetOauth2Config(provider string, localBaseURL string) oauth2.Config {
	var config oauth2.Config
	switch strings.ToLower(provider) {
	case "github":
		config = oauth2.Config{
			ClientID:     os.Getenv("GITHUB_KEY"),
			ClientSecret: os.Getenv("GITHUB_SECRET"),
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://github.com/login/oauth/authorize",
				TokenURL: "https://github.com/login/oauth/access_token",
			},
		}
	case "gitlab":
		config = oauth2.Config{
			ClientID:     os.Getenv("GITLAB_KEY"),
			ClientSecret: os.Getenv("GITLAB_SECRET"),
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://gitlab.com/login/oauth/authorize",
				TokenURL: "https://gitlab.com/login/oauth/access_token",
			},
		}
	case "facebook":
		config = oauth2.Config{
			ClientID:     os.Getenv("FACEBOOK_KEY"),
			ClientSecret: os.Getenv("FACEBOOK_SECRET"),
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://graph.facebook.com/oauth/authorize",
				TokenURL: "https://graph.facebook.com/oauth/access_token",
			},
		}
	case "google":
		config = oauth2.Config{
			ClientID:     os.Getenv("GOOGLE_KEY"),
			ClientSecret: os.Getenv("GOOGLE_SECRET"),
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://accounts.google.com/o/oauth2/auth", // access_type=offline
				TokenURL: "https://accounts.google.com/o/oauth2/token",
			},
		}
	case "fedbox":
		fallthrough
	default:
		apiURL := os.Getenv("API_URL")
		config = oauth2.Config{
			ClientID:     os.Getenv("OAUTH2_KEY"),
			ClientSecret: os.Getenv("OAUTH2_SECRET"),
			Endpoint: oauth2.Endpoint{
				AuthURL:  fmt.Sprintf("%s/oauth/authorize", apiURL),
				TokenURL: fmt.Sprintf("%s/oauth/token", apiURL),
			},
		}
	}
	confOauth2URL := os.Getenv("OAUTH2_URL")
	if u, err := url.Parse(confOauth2URL); err != nil || u.Host == "" {
		config.RedirectURL = fmt.Sprintf("%s/auth/%s/callback", localBaseURL, provider)
	}
	return config
}

func isInverted(r *http.Request) bool {
	cookies := r.Cookies()
	for _, c := range cookies {
		if c.Name == "inverted" {
			return true
		}
	}
	return false
}

func loadCurrentAccountFromSession(s *sessions.Session, l log.Logger) Account {
	// load the current account from the session or setting it to anonymous
	raw, ok := s.Values[SessionUserKey]
	if !ok {
		return defaultAccount
	}
	a, ok := raw.(Account)
	if !ok {
		return defaultAccount
	}
	l.WithContext(log.Ctx{
		"handle": a.Handle,
		"hash":   a.Hash,
	}).Debug("loaded account from session")
	return a
}

func SetSecurityHeaders(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline';")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Xss-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

func (h *handler) LoadSession(next http.Handler) http.Handler {
	if !h.conf.SessionsEnabled {
		return next
	}
	fn := func(w http.ResponseWriter, r *http.Request) {
		h.storage.WithAccount(nil)
		if h.v.s == nil {
			h.logger.Warn("missing session store, unable to load session")
			return
		}
		s, err := h.v.s.get(r)
		if err != nil {
			h.logger.WithContext(log.Ctx{
				"err": err,
			}).Error("unable to load session")
			if xerrors.Is(err, new(os.PathError)) {
				h.v.s.new(r)
			}
		}
		acc := loadCurrentAccountFromSession(s, h.logger)
		m := acc.Metadata
		if acc.IsLogged() {
			f := &Filters{
				Name: CompStrs{EqualsString(acc.Handle)},
				//IRI:  CompStrs{EqualsString(acc.Hash.String())},
				Type: ActivityTypesFilter(ValidActorTypes...),
			}
			accounts, err := h.storage.accounts(f)
			ctx := log.Ctx{
				"handle": acc.Handle,
				"hash":   acc.Hash,
			}
			if err != nil {
				h.v.s.new(r)
				ctxtErr(next, w, r, err)
				return
			}
			if len(accounts) == 0 {
				h.v.s.new(r)
				err := errors.NotFoundf("no accounts found for %v", f)
				h.logger.WithContext(ctx).Warnf(err.Error())
				ctxtErr(next, w, r, err)
				return
			}
			if !accounts[0].IsValid() {
				ctxtErr(next, w, r, errors.NotFoundf("Not found"))
				return
			}
			acc = accounts[0]
			// TODO(marius): Fix this ugly hack where we need to not override OAuth2 metadata loaded at login
			acc.Metadata = m
			h.storage.WithAccount(&acc)
			// TODO(marius): this needs to be moved to where we're handling all Inbox activities, not on page load
			acc, err = h.storage.loadAccountsFollowers(acc)
			if err != nil {
				h.logger.WithContext(ctx).Warnf(err.Error())
			}
			acc, err = h.storage.loadAccountsFollowing(acc)
			if err != nil {
				h.logger.WithContext(ctx).Warnf(err.Error())
			}
			acc, err = h.storage.loadAccountsBlockedIgnored(acc)
			if err != nil {
				h.logger.WithContext(ctx).Warnf(err.Error())
			}

			var items ItemCollection
			if cursor := ContextCursor(r.Context()); cursor != nil {
				items = cursor.items.Items()
			}
			h.storage.loadAccountVotes(&acc, items)
			acc, err = h.storage.loadAccountsOutbox(acc)
			if err != nil {
				h.logger.WithContext(ctx).Warnf(err.Error())
			}
			r = r.WithContext(context.WithValue(r.Context(), LoggedAccountCtxtKey, &acc))
			h.storage.WithAccount(&acc)
		}
		next.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

func (h *handler) addFlashErrors(r *http.Request, errs ...error) {
	msg := ""
	for _, err := range errs {
		msg += err.Error()
	}
	h.v.addFlashMessage(Error, r, msg)
}

func (h handler) NeedsSessions(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		if !h.conf.SessionsEnabled {
			h.v.HandleErrors(w, r, errors.NotFoundf("sessions are disabled"))
			return
		}
		next.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

// HandleAbout serves /about request
// It's something Mastodon compatible servers should show
func (h *handler) HandleAbout(w http.ResponseWriter, r *http.Request) {
	m := &aboutModel{Title: "About"}

	repo := h.storage
	info, err := repo.LoadInfo()
	if err != nil {
		h.v.HandleErrors(w, r, errors.NewNotValid(err, "oops!"))
		return
	}
	m.Desc.Description = info.Description

	h.v.RenderTemplate(r, w, m.Template(), m)
}

func httpErrorResponse(e error) int {
	if errors.IsBadRequest(e) {
		return http.StatusBadRequest
	}
	if errors.IsForbidden(e) {
		return http.StatusForbidden
	}
	if errors.IsNotSupported(e) {
		return http.StatusHTTPVersionNotSupported
	}
	if errors.IsMethodNotAllowed(e) {
		return http.StatusMethodNotAllowed
	}
	if errors.IsNotFound(e) {
		return http.StatusNotFound
	}
	if errors.IsNotImplemented(e) {
		return http.StatusNotImplemented
	}
	if errors.IsUnauthorized(e) {
		return http.StatusUnauthorized
	}
	if errors.IsTimeout(e) {
		return http.StatusGatewayTimeout
	}
	if errors.IsNotValid(e) {
		return http.StatusInternalServerError
	}
	return http.StatusInternalServerError
}

func loadEnvSessionKeys() [][]byte {
	keys := make([][]byte, 0)
	if authKey := []byte(os.Getenv("SESS_AUTH_KEY")); len(authKey) > 0 {
		keys = append(keys, authKey)
	}
	if encKey := []byte(os.Getenv("SESS_ENC_KEY")); len(encKey) > 0 {
		keys = append(keys, encKey)
	}
	return keys
}

func (h *handler) ErrorHandler(errs ...error) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		h.v.HandleErrors(w, r, errs...)
	}
	return http.HandlerFunc(fn)
}

func (h handler) CSRF(next http.Handler) http.Handler {
	opts := []csrf.Option{
		csrf.CookieName(csrfName),
		csrf.FieldName(csrfName),
		csrf.Secure(h.conf.Env.IsProd()),
		csrf.ErrorHandler(h.ErrorHandler(errors.Forbiddenf("Invalid request token"))),
	}
	var authKey []byte
	if len(h.conf.SessionKeys) > 0 {
		authKey = h.conf.SessionKeys[0]
	} else {
		if h.conf.Env.IsProd() {
			h.logger.Warnf("Invalid CSRF auth key")
		}
		// TODO(marius): WTF is this?
		authKey = []byte{0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1}
	}
	return csrf.Protect(authKey, opts...)(next)
}
