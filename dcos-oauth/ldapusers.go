package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"github.com/samuel/go-zookeeper/zk"
	"golang.org/x/net/context"

	"github.com/dcos/dcos-oauth/common"
	"regexp"
)

const (
	zkLdapPath = "/dcos/ldapusers"
)

var (
	ldapUserRe = regexp.MustCompile(`^[a-zA-Z0-9“”._-]{2,}$`)
)

func validateLdapUser(uid string) bool {
	//TODO: Need to update the RegExp to reflect LDAP usernames
	return ldapUserRe.MatchString(uid)
}

func isLdapUser(ctx context.Context, uid string) (bool, error) {
	if !ldapLoginEnabled {
		return false, nil
	}

	c := ctx.Value("zk").(common.IZk)

	path := fmt.Sprintf("%s/%s", zkLdapPath, uid)
	exists, _, err := c.Exists(path)
	if err != nil {
		return false, err
	}

	return exists, nil
}

func addLdapUser(ctx context.Context, uid string) error {
	if !ldapLoginEnabled {
		return nil
	}

	c := ctx.Value("zk").(common.IZk)

	path := fmt.Sprintf("%s/%s", zkLdapPath, uid)
	exists, _, err := c.Exists(path)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("LDAP user already exists: %s", uid)
	}

	err = common.CreateParents(c, path, []byte(uid))
	if err != nil {
		return err
	}
	log.Printf("LDAP user created: %s", uid)

	return nil
}

func getLdapUsers(ctx context.Context, w http.ResponseWriter, r *http.Request) *common.HttpError {
	c := ctx.Value("zk").(common.IZk)
	users, _, err := c.Children(zkLdapPath)
	if err != nil && err != zk.ErrNoNode {
		return common.NewHttpError("invalid LDAP path", http.StatusInternalServerError)
	}

	// users will be an empty list on ErrNoNode
	var usersJson Users
	for _, user := range users {
		userJson := &User{
			Uid:         user,
			Description: user,
			URL:         "",
			IsRemote:    false,
		}
		usersJson.Array = append(usersJson.Array, userJson)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(usersJson)
	log.Debugf("LDAP users listed: %+v\n", users)
	return nil
}

func getLdapUser(ctx context.Context, w http.ResponseWriter, r *http.Request) *common.HttpError {
	// uid is already unescaped here
	uid := mux.Vars(r)["uid"]
	if !validateLdapUser(uid) {
		return common.NewHttpError("invalid LDAP user", http.StatusInternalServerError)
	}

	c := ctx.Value("zk").(common.IZk)

	path := fmt.Sprintf("%s/%s", zkLdapPath, uid)
	exists, _, err := c.Exists(path)
	if err != nil {
		return common.NewHttpError("Zookeeper error", http.StatusInternalServerError)
	}
	if !exists {
		log.Printf("getLocalUser: %v doesn't exist", path)
		return common.NewHttpError("LDAP User Not Found", http.StatusNotFound)
	}

	w.Header().Set("Content-Type", "application/json")
	userJson := &User{
		Uid:         uid,
		Description: uid,
		IsRemote:    false,
	}
	json.NewEncoder(w).Encode(userJson)

	log.Debugf("LDAP user listed: %+v\n", uid)

	return nil
}

func putLdapUsers(ctx context.Context, w http.ResponseWriter, r *http.Request) *common.HttpError {
	if !ldapLoginEnabled {
		return common.NewHttpError("LDAP login not enabled", http.StatusServiceUnavailable)
	}

	if !ldapWhitelistOnly {
		return common.NewHttpError("LDAP user created automatically at login", http.StatusServiceUnavailable)
	}

	uid := mux.Vars(r)["uid"]
	if !validateLdapUser(uid) {
		return common.NewHttpError("invalid LDAP user", http.StatusInternalServerError)
	}

	c := ctx.Value("zk").(common.IZk)

	path := fmt.Sprintf("%s/%s", zkLdapPath, uid)
	exists, _, err := c.Exists(path)
	if err != nil {
		return common.NewHttpError("Zookeeper error", http.StatusInternalServerError)
	}
	if exists {
		return common.NewHttpError("Already Exists", http.StatusConflict)
	}

	var user User
	err = json.NewDecoder(r.Body).Decode(&user)
	if err != nil {
		log.Debugf("putLdapUsers: Decode: %v", err)
		return common.NewHttpError("invalid user json", http.StatusBadRequest)
	}
	log.Printf("Create LDAP user: %+v", user)

	err = common.CreateParents(c, path, []byte(uid))
	if err != nil {
		return common.NewHttpError("Zookeeper error", http.StatusInternalServerError)
	}
	w.WriteHeader(http.StatusCreated)

	log.Debugf("LDAP user created: %+v", uid)

	return nil
}

func deleteLdapUsers(ctx context.Context, w http.ResponseWriter, r *http.Request) *common.HttpError {
	uid := mux.Vars(r)["uid"]
	if !validateLdapUser(uid) {
		return common.NewHttpError("invalid LDAP user", http.StatusInternalServerError)
	}

	c := ctx.Value("zk").(common.IZk)
	path := fmt.Sprintf("%s/%s", zkLdapPath, uid)
	exists, _, err := c.Exists(path)
	if err != nil {
		return common.NewHttpError("Zookeeper error", http.StatusInternalServerError)
	}
	if !exists {
		return common.NewHttpError("LDAP user not found", http.StatusNotFound)
	}

	err = c.Delete(path, 0)
	if err != nil {
		return common.NewHttpError("Zookeeper error", http.StatusInternalServerError)
	}

	w.WriteHeader(http.StatusNoContent)
	log.Printf("LDAP user deleted: %+v", uid)
	return nil
}
