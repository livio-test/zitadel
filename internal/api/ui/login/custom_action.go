package login

import (
	"context"
	"encoding/json"

	"github.com/dop251/goja"
	"github.com/zitadel/logging"
	"github.com/zitadel/oidc/v2/pkg/oidc"
	"golang.org/x/text/language"

	"github.com/zitadel/zitadel/internal/actions"
	"github.com/zitadel/zitadel/internal/actions/object"
	"github.com/zitadel/zitadel/internal/api/authz"
	"github.com/zitadel/zitadel/internal/domain"
	iam_model "github.com/zitadel/zitadel/internal/iam/model"
)

func (l *Login) customExternalUserMapping(ctx context.Context, user *domain.ExternalUser, tokens *oidc.Tokens, req *domain.AuthRequest, config *iam_model.IDPConfigView) (*domain.ExternalUser, error) {
	resourceOwner := req.RequestedOrgID
	if resourceOwner == "" {
		resourceOwner = config.AggregateID
	}
	instance := authz.GetInstance(ctx)
	if resourceOwner == instance.InstanceID() {
		resourceOwner = instance.DefaultOrganisationID()
	}
	triggerActions, err := l.query.GetActiveActionsByFlowAndTriggerType(ctx, domain.FlowTypeExternalAuthentication, domain.TriggerTypePostAuthentication, resourceOwner, false)
	if err != nil {
		return nil, err
	}

	mutableMetas := &metas{m: user.Metadatas}
	apiFields := actions.WithAPIFields(
		actions.SetFields("setFirstName", func(firstName string) {
			user.FirstName = firstName
		}),
		actions.SetFields("setLastName", func(lastName string) {
			user.LastName = lastName
		}),
		actions.SetFields("setNickName", func(nickName string) {
			user.NickName = nickName
		}),
		actions.SetFields("setDisplayName", func(displayName string) {
			user.DisplayName = displayName
		}),
		actions.SetFields("setPreferredLanguage", func(preferredLanguage string) {
			user.PreferredLanguage = language.Make(preferredLanguage)
		}),
		actions.SetFields("setPreferredUsername", func(username string) {
			user.PreferredUsername = username
		}),
		actions.SetFields("setEmail", func(email string) {
			user.Email = email
		}),
		actions.SetFields("setEmailVerified", func(verified bool) {
			user.IsEmailVerified = verified
		}),
		actions.SetFields("setPhone", func(phone string) {
			user.Phone = phone
		}),
		actions.SetFields("setPhoneVerified", func(verified bool) {
			user.IsPhoneVerified = verified
		}),
		actions.SetFields("metadata", &user.Metadatas),
		actions.SetFields("v1",
			actions.SetFields("user",
				actions.SetFields("appendMetadata", appendMetadataFunc(mutableMetas)),
			),
			actions.SetFields("mgmt", object.ManagementField(l.mgmtServer)),
		),
	)

	for _, a := range triggerActions {
		actionCtx, cancel := context.WithTimeout(ctx, a.Timeout())

		ctxFieldOptions := append(tokenCtxFields(tokens),
			actions.SetFields("v1",
				actions.SetFields("ctx", actionCtx),
				actions.SetFields("externalUser", func(c *actions.FieldConfig) interface{} {
					return object.UserFromExternalUser(c, user)
				}),
				actions.SetFields("authRequest", object.AuthRequestField(req)),
			),
		)

		ctxFields := actions.SetContextFields(ctxFieldOptions...)

		err = actions.Run(
			actionCtx,
			ctxFields,
			apiFields,
			a.Script,
			a.Name,
			append(actions.ActionToOptions(a), actions.WithHTTP(actionCtx), actions.WithLogger(actions.ServerLog))...,
		)
		cancel()
		if err != nil {
			return nil, err
		}
	}
	user.Metadatas = mutableMetas.m
	return user, err
}

func (l *Login) triggerPostLocalAuthentication(ctx context.Context, req *domain.AuthRequest, authenticationError error) error {
	resourceOwner := req.RequestedOrgID
	if resourceOwner == "" {
		resourceOwner = req.UserOrgID
	}

	triggerActions, err := l.query.GetActiveActionsByFlowAndTriggerType(ctx, domain.FlowTypeInternalAuthentication, domain.TriggerTypePostAuthentication, resourceOwner, false)
	if err != nil {
		return err
	}

	apiFields := actions.WithAPIFields(
		actions.SetFields("v1",
			actions.SetFields("mgmt", l.mgmtServer),
		),
	)

	for _, a := range triggerActions {
		actionCtx, cancel := context.WithTimeout(ctx, a.Timeout())

		authErrStr := ""
		if authenticationError != nil {
			authErrStr = authenticationError.Error()
		}

		ctxFields := actions.SetContextFields(
			// TODO: add tokenCtxFields(tokens)
			actions.SetFields("v1",
				actions.SetFields("ctx", actionCtx),
				actions.SetFields("authenticationError", authErrStr),
				actions.SetFields("authRequest", object.AuthRequestField(req)),
			),
		)

		err = actions.Run(
			actionCtx,
			ctxFields,
			apiFields,
			a.Script,
			a.Name,
			append(actions.ActionToOptions(a), actions.WithHTTP(actionCtx), actions.WithLogger(actions.ServerLog))...,
		)
		cancel()
		if err != nil {
			return err
		}
	}
	return err
}

func (l *Login) customUserToLoginUserMapping(ctx context.Context, authRequest *domain.AuthRequest, user *domain.Human, metadata []*domain.Metadata, resourceOwner string, flowType domain.FlowType) (*domain.Human, []*domain.Metadata, error) {
	triggerActions, err := l.query.GetActiveActionsByFlowAndTriggerType(ctx, flowType, domain.TriggerTypePreCreation, resourceOwner, false)
	if err != nil {
		return nil, nil, err
	}

	mutableMetas := &metas{m: metadata}

	apiFields := actions.WithAPIFields(
		actions.SetFields("setFirstName", func(firstName string) {
			user.FirstName = firstName
		}),
		actions.SetFields("setLastName", func(lastName string) {
			user.LastName = lastName
		}),
		actions.SetFields("setNickName", func(nickName string) {
			user.NickName = nickName
		}),
		actions.SetFields("setDisplayName", func(displayName string) {
			user.DisplayName = displayName
		}),
		actions.SetFields("setPreferredLanguage", func(preferredLanguage string) {
			user.PreferredLanguage = language.Make(preferredLanguage)
		}),
		actions.SetFields("setGender", func(gender domain.Gender) {
			user.Gender = gender
		}),
		actions.SetFields("setUsername", func(username string) {
			user.Username = username
		}),
		actions.SetFields("setEmail", func(email string) {
			if user.Email == nil {
				user.Email = &domain.Email{}
			}
			user.Email.EmailAddress = email
		}),
		actions.SetFields("setEmailVerified", func(verified bool) {
			if user.Email == nil {
				return
			}
			user.Email.IsEmailVerified = verified
		}),
		actions.SetFields("setPhone", func(email string) {
			if user.Phone == nil {
				user.Phone = &domain.Phone{}
			}
			user.Phone.PhoneNumber = email
		}),
		actions.SetFields("setPhoneVerified", func(verified bool) {
			if user.Phone == nil {
				return
			}
			user.Phone.IsPhoneVerified = verified
		}),
		actions.SetFields("metadata", metadata),
		actions.SetFields("v1",
			actions.SetFields("user",
				actions.SetFields("appendMetadata", appendMetadataFunc(mutableMetas)),
			),
			actions.SetFields("mgmt", object.ManagementField(l.mgmtServer)),
		),
	)

	for _, a := range triggerActions {
		actionCtx, cancel := context.WithTimeout(ctx, a.Timeout())

		ctxOpts := actions.SetContextFields(
			actions.SetFields("v1",
				actions.SetFields("user", func(c *actions.FieldConfig) interface{} {
					return object.UserFromHuman(c, user)
				}),
				actions.SetFields("ctx", actionCtx),
				actions.SetFields("authRequest", object.AuthRequestField(authRequest)),
			),
		)

		err = actions.Run(
			actionCtx,
			ctxOpts,
			apiFields,
			a.Script,
			a.Name,
			append(actions.ActionToOptions(a), actions.WithHTTP(actionCtx), actions.WithLogger(actions.ServerLog))...,
		)
		cancel()
		if err != nil {
			return nil, nil, err
		}
	}
	return user, mutableMetas.m, err
}

func (l *Login) customGrants(ctx context.Context, userID string, authRequest *domain.AuthRequest, resourceOwner string, flowType domain.FlowType) ([]*domain.UserGrant, error) {
	triggerActions, err := l.query.GetActiveActionsByFlowAndTriggerType(ctx, flowType, domain.TriggerTypePostCreation, resourceOwner, false)
	if err != nil {
		return nil, err
	}

	actionUserGrants := make([]actions.UserGrant, 0)

	apiFields := actions.WithAPIFields(
		actions.SetFields("userGrants", &actionUserGrants),
		actions.SetFields("v1",
			actions.SetFields("appendUserGrant", func(c *actions.FieldConfig) interface{} {
				return func(call goja.FunctionCall) goja.Value {
					if len(call.Arguments) != 1 {
						panic("exactly one argument expected")
					}
					object := call.Arguments[0].ToObject(c.Runtime)
					if object == nil {
						panic("unable to unmarshal arg")
					}
					grant := actions.UserGrant{}

					for _, key := range object.Keys() {
						switch key {
						case "projectId":
							grant.ProjectID = object.Get(key).String()
						case "projectGrantId":
							grant.ProjectGrantID = object.Get(key).String()
						case "roles":
							if roles, ok := object.Get(key).Export().([]interface{}); ok {
								for _, role := range roles {
									if r, ok := role.(string); ok {
										grant.Roles = append(grant.Roles, r)
									}
								}
							}
						}
					}

					if grant.ProjectID == "" {
						panic("projectId not set")
					}

					actionUserGrants = append(actionUserGrants, grant)

					return nil
				}
			}),
			actions.SetFields("mgmt", l.mgmtServer),
		),
	)

	for _, a := range triggerActions {
		actionCtx, cancel := context.WithTimeout(ctx, a.Timeout())

		ctxFields := actions.SetContextFields(
			actions.SetFields("v1",
				actions.SetFields("getUser", func(c *actions.FieldConfig) interface{} {
					return func(call goja.FunctionCall) goja.Value {
						user, err := l.query.GetUserByID(actionCtx, true, authRequest.UserID, false)
						if err != nil {
							panic(err)
						}
						return object.UserFromQuery(c, user)
					}
				}),
				actions.SetFields("ctx", actionCtx),
				actions.SetFields("authRequest", object.AuthRequestField(authRequest)),
			),
		)

		err = actions.Run(
			actionCtx,
			ctxFields,
			apiFields,
			a.Script,
			a.Name,
			append(actions.ActionToOptions(a), actions.WithHTTP(actionCtx), actions.WithLogger(actions.ServerLog))...,
		)
		cancel()
		if err != nil {
			return nil, err
		}
	}
	return actionUserGrantsToDomain(userID, actionUserGrants), err
}

func tokenCtxFields(tokens *oidc.Tokens) []actions.FieldOption {
	return []actions.FieldOption{
		actions.SetFields("accessToken", tokens.AccessToken),
		actions.SetFields("idToken", tokens.IDToken),
		actions.SetFields("getClaim", func(claim string) interface{} {
			return tokens.IDTokenClaims.GetClaim(claim)
		}),
		actions.SetFields("claimsJSON", func() (string, error) {
			// TODO: why don't we return the struct?
			c, err := json.Marshal(tokens.IDTokenClaims)
			if err != nil {
				return "", err
			}
			return string(c), nil
		}),
	}
}

func actionUserGrantsToDomain(userID string, actionUserGrants []actions.UserGrant) []*domain.UserGrant {
	if actionUserGrants == nil {
		return nil
	}
	userGrants := make([]*domain.UserGrant, len(actionUserGrants))
	for i, grant := range actionUserGrants {
		userGrants[i] = &domain.UserGrant{
			UserID:         userID,
			ProjectID:      grant.ProjectID,
			ProjectGrantID: grant.ProjectGrantID,
			RoleKeys:       grant.Roles,
		}
	}
	return userGrants
}

type metas struct {
	m []*domain.Metadata
}

func appendMetadataFunc(mutableMetas *metas) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) != 2 {
			panic("exactly 2 (key, value) arguments expected")
		}
		key := call.Arguments[0].Export().(string)
		val := call.Arguments[1].Export()

		value, err := json.Marshal(val)
		if err != nil {
			logging.WithError(err).Debug("unable to marshal")
			panic(err)
		}

		mutableMetas.m = append(mutableMetas.m,
			&domain.Metadata{
				Key:   key,
				Value: value,
			})
		return nil
	}
}
