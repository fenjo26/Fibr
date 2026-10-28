# LuCI application package for Fibr: sets up a WireGuard tunnel to a Fibr
# server from a quick-connect join code (or a pasted wg-quick .conf) and
# manages it (status, on/off) from a single page.
#
# The Makefile is deliberately self-contained (plain include/package.mk, no
# luci.mk): this package lives outside the LuCI feed, and luci.mk's package
# type detection assumes feed-internal directory paths. The install layout
# follows the luci-app-wireguard conventions exactly:
#
#   htdocs/luci-static/resources/view/fibr.js -> /www/luci-static/resources/view/
#   root/usr/share/luci/menu.d/*.json         -> menu entry (Network -> Fibr VPN)
#   root/usr/share/rpcd/acl.d/*.json          -> rpcd ACL for the page
#   root/etc/uci-defaults/99-fibr             -> first-boot/install defaults

include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-fibr
PKG_VERSION:=1.0.0
PKG_RELEASE:=1
PKG_LICENSE:=MIT
PKG_MAINTAINER:=Fibr project <https://github.com/fenjo26/Fibr>

PKG_BUILD_DIR:=$(BUILD_DIR)/$(PKG_NAME)-$(PKG_VERSION)

include $(INCLUDE_DIR)/package.mk

define Package/luci-app-fibr
  SECTION:=luci
  CATEGORY:=LuCI
  SUBMENU:=3. Applications
  TITLE:=Fibr VPN tunnel setup
  URL:=https://github.com/fenjo26/Fibr
  DEPENDS:=+luci-base +luci-proto-wireguard +wireguard-tools
  PKGARCH:=all
endef

define Package/luci-app-fibr/description
  Configures a WireGuard tunnel to a Fibr server on this router. Enter a
  quick-connect join code from the Fibr Mini App (the plugin exchanges it
  for the config directly with the server) or paste the .conf file, and the
  plugin writes the network.wg_fibr interface, the wireguard_wg_fibr peer
  with AllowedIPs from the config, and a masquerading firewall zone with
  lan->fibr forwarding. Status and enable/disable toggling on the same page.
  OpenWrt 23.05+ (24.10 recommended).
endef

# Data-only package: no sources to build.

define Build/Prepare
	mkdir -p $(PKG_BUILD_DIR)
endef

define Build/Configure
endef

define Build/Compile
endef

define Package/luci-app-fibr/install
	$(INSTALL_DIR) $(1)/www/luci-static/resources/view
	$(INSTALL_DATA) ./htdocs/luci-static/resources/view/fibr.js \
		$(1)/www/luci-static/resources/view/fibr.js
	$(INSTALL_DIR) $(1)/usr/share/luci/menu.d
	$(INSTALL_DATA) ./root/usr/share/luci/menu.d/luci-app-fibr.json \
		$(1)/usr/share/luci/menu.d/luci-app-fibr.json
	$(INSTALL_DIR) $(1)/usr/share/rpcd/acl.d
	$(INSTALL_DATA) ./root/usr/share/rpcd/acl.d/luci-app-fibr.json \
		$(1)/usr/share/rpcd/acl.d/luci-app-fibr.json
	$(INSTALL_DIR) $(1)/etc/uci-defaults
	$(INSTALL_DATA) ./root/etc/uci-defaults/99-fibr \
		$(1)/etc/uci-defaults/99-fibr
endef

# On a live router: reload rpcd so it picks up the new ACL file, then run
# the uci-defaults script (it does the same for first-boot image installs,
# where this postinst runs inside the image root and skips).
define Package/luci-app-fibr/postinst
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] || {
	killall -HUP rpcd 2>/dev/null || true
	for file in /etc/uci-defaults/*; do
		[ -f "$$file" ] || continue
		sh "$$file" && rm -f "$$file"
	done
}
exit 0
endef

$(eval $(call BuildPackage,luci-app-fibr))
