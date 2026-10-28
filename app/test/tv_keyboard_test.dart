import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:fibr/services/app_list_service.dart';
import 'package:fibr/widgets/tv_keyboard.dart';

void main() {
  // The isTv group mocks the fibr/apps MethodChannel — needs the binding.
  TestWidgetsFlutterBinding.ensureInitialized();

  group('TvCodeEntry', () {
    test('append advances the segment cursor', () {
      final e = TvCodeEntry();
      expect(e.cursor, 0);
      expect(e.append('A'), isTrue);
      expect(e.cursor, 1);
      expect(e.text, 'A');
      expect(e.charAt(0), 'A');
      expect(e.charAt(1), ''); // not typed yet
    });

    test('rejects input beyond the fixed length', () {
      final e = TvCodeEntry(length: 8);
      for (var i = 0; i < 8; i++) {
        expect(e.append(i.toString()), isTrue, reason: 'char #$i');
      }
      expect(e.isFull, isTrue);
      expect(e.isComplete, isTrue);
      expect(e.append('9'), isFalse);
      expect(e.text, hasLength(8));
    });

    test('append of an empty string is a no-op', () {
      final e = TvCodeEntry();
      expect(e.append(''), isFalse);
      expect(e.isEmpty, isTrue);
    });

    test('backspace removes the last char and steps back', () {
      final e = TvCodeEntry();
      e.append('a');
      e.append('b');
      expect(e.backspace(), isTrue);
      expect(e.text, 'a');
      expect(e.cursor, 1);
      e.backspace();
      expect(e.isEmpty, isTrue);
      expect(e.backspace(), isFalse); // nothing left to delete
    });

    test('clear empties the buffer', () {
      final e = TvCodeEntry()..append('x')..append('y');
      e.clear();
      expect(e.isEmpty, isTrue);
      expect(e.isComplete, isFalse);
    });

    test('charAt tolerates a negative index', () {
      expect(TvCodeEntry().charAt(-1), '');
    });
  });

  group('TvKeyboardLayout', () {
    // The server's lookalike-free join-code alphabet (api/invites.go).
    const serverAlphabet =
        '23456789abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ';

    test('code layout can type any server join code', () {
      for (final ch in serverAlphabet.split('')) {
        expect(TvKeyboardLayout.code.allChars.contains(ch), isTrue,
            reason: 'missing key for "$ch"');
      }
      // And nothing outside the alphabet — a typo cannot be typed at all.
      for (final ch in TvKeyboardLayout.code.allChars.split('')) {
        expect(serverAlphabet.contains(ch), isTrue,
            reason: 'unexpected key "$ch"');
      }
    });

    test('domain layout covers hosts and ports', () {
      final all = TvKeyboardLayout.domain.allChars;
      for (final ch
          in 'abcdefghijklmnopqrstuvwxyz0123456789.-:'.split('')) {
        expect(all.contains(ch), isTrue, reason: 'missing key for "$ch"');
      }
      // A domain never needs a space or a slash.
      expect(all.contains(' '), isFalse);
      expect(all.contains('/'), isFalse);
    });

    test('rows stay remote-navigable (non-empty, at most 10 keys)', () {
      for (final layout in [TvKeyboardLayout.code, TvKeyboardLayout.domain]) {
        expect(layout.digitRow, isNotEmpty);
        expect(layout.digitRow.length, lessThanOrEqualTo(10));
        for (final row in layout.letterRows) {
          expect(row, isNotEmpty);
          expect(row.length, lessThanOrEqualTo(10));
        }
      }
    });
  });

  group('AppListService.isTv', () {
    const channel = MethodChannel('fibr/apps');
    final messenger =
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;

    setUp(() {
      debugDefaultTargetPlatformOverride = TargetPlatform.android;
    });

    tearDown(() async {
      messenger.setMockMethodCallHandler(channel, null);
      debugDefaultTargetPlatformOverride = null;
    });

    test('relays the native UiModeManager answer (true)', () async {
      messenger.setMockMethodCallHandler(channel, (call) async {
        if (call.method == 'isTv') return true;
        return null;
      });
      expect(await AppListService().isTv(), isTrue);
    });

    test('relays the native UiModeManager answer (false)', () async {
      messenger.setMockMethodCallHandler(channel, (call) async {
        if (call.method == 'isTv') return false;
        return null;
      });
      expect(await AppListService().isTv(), isFalse);
    });

    test('null answer folds into false', () async {
      messenger.setMockMethodCallHandler(channel, (call) async => null);
      expect(await AppListService().isTv(), isFalse);
    });

    test('no channel handler (iOS/desktop) is false, not a crash', () async {
      expect(await AppListService().isTv(), isFalse);
    });

    test('non-Android platforms answer false without touching the channel',
        () async {
      debugDefaultTargetPlatformOverride = TargetPlatform.iOS;
      expect(await AppListService().isTv(), isFalse);
    });
  });
}
