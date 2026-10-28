package api

// Reseller hierarchy and data scoping (see docs/roles.md):
//
//	superadmin — the installation owner: the static FIBR_ADMIN_TOKEN and the
//	            telegram ids from FIBR_ADMIN_TELEGRAM_IDS. Sees and manages
//	            everything, including /api/admin/overview.
//	admin      — a reseller/sub-admin. Creates plain users (owner_id = their
//	            id) and manages only those users and their devices. Cannot
//	            change roles, cannot create admins, gets 404 for anyone else.
//	user       — an end account: their own devices, QR and stats via /api/me.
//
// The scoping helpers below are the single place where the "who may see
// which account" rules live; every /api/admin handler funnels through them.

// visibleUserIDs returns the user ids whose accounts and devices the caller
// may see: nil (= unrestricted, all users) for the superadmin, or the admin's
// own id plus the ids of the users they created.
func (a *API) visibleUserIDs(id identity) ([]int64, error) {
	if id.isSuper() {
		return nil, nil
	}
	ids := []int64{id.userID}
	children, err := a.store.ListUserIDsByOwner(id.userID)
	if err != nil {
		return nil, err
	}
	return append(ids, children...), nil
}
