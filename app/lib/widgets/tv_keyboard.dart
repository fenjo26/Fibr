import 'package:flutter/material.dart';

import '../theme/app_theme.dart';

/// Fixed-length segmented code buffer — pure logic, unit-tested in
/// `test/tv_keyboard_test.dart`. Append advances the cursor to the next
/// segment; backspace removes the last char and steps the cursor back.
class TvCodeEntry {
  TvCodeEntry({this.length = 8});

  /// Server join codes are 8 chars (api/join.go `joinCodeLen`).
  final int length;
  final List<String> _chars = <String>[];

  String get text => _chars.join();

  /// Segment index the next typed char lands in (0..length).
  int get cursor => _chars.length;

  bool get isEmpty => _chars.isEmpty;
  bool get isFull => _chars.length >= length;
  bool get isComplete => _chars.length == length;

  /// Appends one char; returns false (and does nothing) when already full
  /// or [ch] is empty.
  bool append(String ch) {
    if (isFull || ch.isEmpty) return false;
    _chars.add(ch.substring(ch.length - 1));
    return true;
  }

  /// Removes the last char; returns false when empty.
  bool backspace() {
    if (isEmpty) return false;
    _chars.removeLast();
    return true;
  }

  void clear() => _chars.clear();

  /// The char in segment [i], or '' when not typed yet.
  String charAt(int i) =>
      i >= 0 && i < _chars.length ? _chars[i] : '';
}

/// Character set + row arrangement of the on-screen keyboard for a target
/// field. Letter rows are stored uppercase; the keyboard renders them in
/// its current case mode.
class TvKeyboardLayout {
  const TvKeyboardLayout._(
    this.digitRow,
    this.letterRows,
    this.defaultUpper,
  );

  final String digitRow;
  final List<String> letterRows;
  final bool defaultUpper;

  /// Every printable glyph the keyboard offers (both cases + digits).
  String get allChars {
    final buf = StringBuffer()..write(digitRow);
    for (final row in letterRows) {
      buf.write(row);
      buf.write(row.toLowerCase());
    }
    return buf.toString();
  }

  /// Join codes: 8 chars from the server's lookalike-free alphabet
  /// (api/invites.go `inviteAlphabet` — no 0/1/O/I/L). Uppercase-first:
  /// codes are mixed-case, uppercase glyphs read better at 10 feet.
  static const code = TvKeyboardLayout._(
    '23456789',
    ['ABCDEFGH', 'JKMNPQRS', 'TUVWXYZ'],
    true,
  );

  /// Server domains: full alphabet plus `.`, `-`, `:` (a port may follow
  /// the host; the scheme is added by the client). Lowercase-first —
  /// domains are lowercase by convention.
  static const domain = TvKeyboardLayout._(
    '0123456789',
    ['ABCDEFGHIJ', 'KLMNOPQRST', 'UVWXYZ.-:'],
    false,
  );
}

/// Remote-friendly on-screen keyboard for the TV onboarding (feature 2):
/// large focusable keys with the secondary (#0075ff) focus ring, a case
/// toggle and a backspace. D-pad focus stays on the pressed key so typing
/// is row-by-row, phone-keyboard style; the screen decides where the chars
/// go (`onKey`/`onDelete` target the active field).
class TvOnScreenKeyboard extends StatefulWidget {
  const TvOnScreenKeyboard({
    super.key,
    required this.layout,
    required this.onKey,
    required this.onDelete,
  });

  final TvKeyboardLayout layout;
  final void Function(String char) onKey;
  final VoidCallback onDelete;

  @override
  State<TvOnScreenKeyboard> createState() => _TvOnScreenKeyboardState();
}

class _TvOnScreenKeyboardState extends State<TvOnScreenKeyboard> {
  late bool _upper = widget.layout.defaultUpper;

  @override
  void didUpdateWidget(TvOnScreenKeyboard oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.layout != widget.layout) {
      _upper = widget.layout.defaultUpper;
    }
  }

  static const _gap = 8.0;
  static const _keyW = 64.0;
  static const _keyH = 56.0;

  @override
  Widget build(BuildContext context) {
    final digitRow = widget.layout.digitRow.split('');
    final letterRows = widget.layout.letterRows
        .map((r) => (_upper ? r : r.toLowerCase()).split(''))
        .toList();

    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _row([for (final ch in digitRow) _key(ch, () => widget.onKey(ch))]),
        const SizedBox(height: _gap),
        for (final row in letterRows) ...[
          _row([for (final ch in row) _key(ch, () => widget.onKey(ch))]),
          const SizedBox(height: _gap),
        ],
        _row([
          _key(
            _upper ? 'abc' : 'ABC',
            () => setState(() => _upper = !_upper),
            width: _keyW * 1.6,
          ),
          _iconKey(Icons.backspace_outlined, widget.onDelete,
              width: _keyW * 1.6),
        ]),
      ],
    );
  }

  Widget _row(List<Widget> keys) => Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          for (var i = 0; i < keys.length; i++) ...[
            if (i > 0) const SizedBox(width: _gap),
            keys[i],
          ],
        ],
      );

  Widget _key(String label, VoidCallback onTap, {double? width}) => _TvKey(
        onTap: onTap,
        width: width ?? _keyW,
        height: _keyH,
        child: Text(
          label,
          style: const TextStyle(
            fontSize: 22,
            fontWeight: FontWeight.w600,
          ),
        ),
      );

  Widget _iconKey(IconData icon, VoidCallback onTap, {double? width}) =>
      _TvKey(
        onTap: onTap,
        width: width ?? _keyW,
        height: _keyH,
        child: Icon(icon, size: 26, color: FibrColors.of(context).ink),
      );
}

/// One keyboard key: focus ring in `secondary` 3px (2-3px per the TV spec),
/// surface2 fill while focused. InkWell activates on the remote's
/// center/Enter button.
class _TvKey extends StatelessWidget {
  const _TvKey({
    required this.onTap,
    required this.child,
    required this.width,
    required this.height,
  });

  final VoidCallback onTap;
  final Widget child;
  final double width;
  final double height;

  @override
  Widget build(BuildContext context) {
    final c = FibrColors.of(context);
    final radius = BorderRadius.circular(10);
    return Focus(
      child: Builder(
        builder: (context) {
          final focused = Focus.of(context).hasFocus;
          return AnimatedContainer(
            duration: const Duration(milliseconds: 120),
            width: width,
            height: height,
            decoration: BoxDecoration(
              borderRadius: radius,
              border: Border.all(
                color: focused ? c.secondary : c.hairline,
                width: focused ? 3 : 1,
              ),
            ),
            child: Material(
              color: focused ? c.surface2 : c.surface,
              borderRadius: radius,
              clipBehavior: Clip.antiAlias,
              child: InkWell(
                onTap: onTap,
                child: Center(child: child),
              ),
            ),
          );
        },
      ),
    );
  }
}
