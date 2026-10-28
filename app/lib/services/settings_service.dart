import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

enum AppLanguageMode { system, ru, en }

/// Theme + language, persisted in shared_preferences (keys `fibr.theme`,
/// `fibr.lang`). Provided via `provider` at the app root.
class SettingsController extends ChangeNotifier {
  ThemeMode _themeMode = ThemeMode.system;
  AppLanguageMode _language = AppLanguageMode.system;

  ThemeMode get themeMode => _themeMode;
  AppLanguageMode get language => _language;

  /// Locale passed to MaterialApp; null means "follow the system".
  Locale? get resolvedLocale {
    switch (_language) {
      case AppLanguageMode.ru:
        return const Locale('ru');
      case AppLanguageMode.en:
        return const Locale('en');
      case AppLanguageMode.system:
        return null;
    }
  }

  Future<void> load() async {
    final prefs = await SharedPreferences.getInstance();
    _themeMode = _themeModeFromString(prefs.getString(_themeKey));
    _language = _languageFromString(prefs.getString(_langKey));
    notifyListeners();
  }

  Future<void> setThemeMode(ThemeMode mode) async {
    if (mode == _themeMode) return;
    _themeMode = mode;
    notifyListeners();
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_themeKey, mode.name);
  }

  Future<void> setLanguage(AppLanguageMode mode) async {
    if (mode == _language) return;
    _language = mode;
    notifyListeners();
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_langKey, mode.name);
  }

  static const _themeKey = 'fibr.theme';
  static const _langKey = 'fibr.lang';

  static ThemeMode _themeModeFromString(String? s) {
    switch (s) {
      case 'dark':
        return ThemeMode.dark;
      case 'light':
        return ThemeMode.light;
      default:
        return ThemeMode.system;
    }
  }

  static AppLanguageMode _languageFromString(String? s) {
    switch (s) {
      case 'ru':
        return AppLanguageMode.ru;
      case 'en':
        return AppLanguageMode.en;
      default:
        return AppLanguageMode.system;
    }
  }
}
