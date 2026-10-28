import 'package:flutter/material.dart';

import '../theme/app_theme.dart';

/// D-pad-friendly interactive tile for the TV UI (feature 2): the FibrCard
/// look (surface fill, hairline, radius 16) plus a 3px focus ring in
/// `secondary` (#0075ff) while the node holds focus, so remote users can
/// always see where the cursor is. InkWell activates on the remote's
/// center/Enter button out of the box. Wrap EVERY interactive element on a
/// TV screen in this (plain Containers and Gestures are unreachable with a
/// D-pad).
class FocusableTile extends StatelessWidget {
  const FocusableTile({
    super.key,
    this.onTap,
    required this.child,
    this.onFocusChange,
    this.autofocus = false,
    this.selected = false,
    this.padding = const EdgeInsets.all(16),
    this.width,
    this.height,
  });

  /// Null makes the tile a pure focus target (no activation) — used for
  /// TV input fields whose focus selects the on-screen keyboard target.
  final VoidCallback? onTap;
  final Widget child;

  /// Mirrors Focus.onFocusChange — TV screens use it to mark the field the
  /// on-screen keyboard types into, etc.
  final ValueChanged<bool>? onFocusChange;

  final bool autofocus;

  /// Persistent selection state (the active device card): a 2px secondary
  /// outline even when not focused.
  final bool selected;

  final EdgeInsetsGeometry padding;
  final double? width;
  final double? height;

  @override
  Widget build(BuildContext context) {
    final c = FibrColors.of(context);
    final radius = BorderRadius.circular(16);
    return Focus(
      autofocus: autofocus,
      onFocusChange: onFocusChange,
      child: Builder(
        builder: (context) {
          final focused = Focus.of(context).hasFocus;
          return AnimatedContainer(
            duration: const Duration(milliseconds: 120),
            curve: Curves.easeOut,
            width: width,
            height: height,
            decoration: BoxDecoration(
              borderRadius: radius,
              border: Border.all(
                color: focused || selected ? c.secondary : c.hairline,
                width: focused ? 3 : (selected ? 2 : 1),
              ),
            ),
            child: Material(
              color: c.surface,
              borderRadius: radius,
              clipBehavior: Clip.antiAlias,
              child: InkWell(
                onTap: onTap,
                child: Padding(padding: padding, child: child),
              ),
            ),
          );
        },
      ),
    );
  }
}

/// Surface card from tokens.css: surface fill, 1px hairline, radius 16,
/// no elevation — depth comes from canvas → surface, not shadows.
class FibrCard extends StatelessWidget {
  const FibrCard({super.key, required this.child, this.onTap, this.padding = const EdgeInsets.all(16)});

  final Widget child;
  final VoidCallback? onTap;
  final EdgeInsetsGeometry padding;

  @override
  Widget build(BuildContext context) {
    final c = FibrColors.of(context);
    final card = Container(
      decoration: BoxDecoration(
        color: c.surface,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: c.hairline),
      ),
      padding: padding,
      child: child,
    );
    if (onTap == null) return card;
    return Material(
      color: Colors.transparent,
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(16),
        child: card,
      ),
    );
  }
}

enum PillTone { ok, live, off, danger }

/// Status pill (tokens .pill*): radius 999, tinted background.
class StatusPill extends StatelessWidget {
  const StatusPill({super.key, required this.label, this.tone = PillTone.off});

  final String label;
  final PillTone tone;

  @override
  Widget build(BuildContext context) {
    final c = FibrColors.of(context);
    final Color bg;
    final Color fg;
    switch (tone) {
      case PillTone.ok:
        bg = c.primary.withOpacity(0.16);
        fg = c.primaryStrong;
      case PillTone.live:
        bg = c.secondary.withOpacity(0.16);
        fg = c.secondaryStrong;
      case PillTone.danger:
        bg = c.danger.withOpacity(0.14);
        fg = c.danger;
      case PillTone.off:
        bg = c.surface2;
        fg = c.muted;
    }
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 2),
      decoration: BoxDecoration(color: bg, borderRadius: BorderRadius.circular(999)),
      child: Text(
        label,
        style: Theme.of(context)
            .textTheme
            .bodySmall
            ?.copyWith(color: fg, fontWeight: FontWeight.w600, fontSize: 12),
      ),
    );
  }
}

/// 8px status dot (tokens .dot): green with a glow when on, faint when off.
class StatusDot extends StatelessWidget {
  const StatusDot({super.key, required this.on});

  final bool on;

  @override
  Widget build(BuildContext context) {
    final c = FibrColors.of(context);
    return Container(
      width: 8,
      height: 8,
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        color: on ? c.primary : c.faint,
        boxShadow: on ? [BoxShadow(color: c.primary, blurRadius: 6)] : null,
      ),
    );
  }
}

/// Label/value row for config details.
class InfoRow extends StatelessWidget {
  const InfoRow({super.key, required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final c = FibrColors.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 120,
            child: Text(label, style: TextStyle(color: c.muted, fontWeight: FontWeight.w600)),
          ),
          Expanded(
            child: Text(
              value,
              style: const TextStyle(
                fontFamily: 'monospace',
                fontFamilyFallback: ['Courier New'],
                fontSize: 12,
                fontWeight: FontWeight.w600,
              ),
            ),
          ),
        ],
      ),
    );
  }
}
