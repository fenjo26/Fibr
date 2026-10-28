import 'package:flutter/material.dart';

/// Design tokens, mirroring design/tokens.css. Dark is the default theme
/// (near-black canvas, surface ladder for depth), light is the daylight
/// companion. Depth comes from the surface step, not shadows.
class FibrColors {
  const FibrColors({
    required this.primary,
    required this.primaryStrong,
    required this.onPrimary,
    required this.secondary,
    required this.secondaryStrong,
    required this.canvas,
    required this.surface,
    required this.surface2,
    required this.hairline,
    required this.ink,
    required this.muted,
    required this.faint,
    required this.danger,
  });

  final Color primary; // main actions only (Connect)
  final Color primaryStrong;
  final Color onPrimary;
  final Color secondary; // secondary actions / links
  final Color secondaryStrong;
  final Color canvas;
  final Color surface;
  final Color surface2;
  final Color hairline;
  final Color ink;
  final Color muted;
  final Color faint;
  final Color danger;

  static const dark = FibrColors(
    primary: Color(0xFF00B24B),
    primaryStrong: Color(0xFF00CC57),
    onPrimary: Color(0xFFFFFFFF),
    secondary: Color(0xFF0075FF),
    secondaryStrong: Color(0xFF2F8CFF),
    canvas: Color(0xFF141415),
    surface: Color(0xFF2E3035),
    surface2: Color(0xFF26282C),
    hairline: Color(0xFF2D2F33),
    ink: Color(0xFFF0F2F5),
    muted: Color(0xFF9AA1B1),
    faint: Color(0xFF53575F),
    danger: Color(0xFFFF4D4F),
  );

  static const light = FibrColors(
    primary: Color(0xFF00A044),
    primaryStrong: Color(0xFF00B24B),
    onPrimary: Color(0xFFFFFFFF),
    secondary: Color(0xFF0075FF),
    secondaryStrong: Color(0xFF0062D6),
    canvas: Color(0xFFF4F5F7),
    surface: Color(0xFFFFFFFF),
    surface2: Color(0xFFEDEFF2),
    hairline: Color(0xFFE2E5EA),
    ink: Color(0xFF191B1F),
    muted: Color(0xFF5D6470),
    faint: Color(0xFF9AA1B1),
    danger: Color(0xFFD92C2C),
  );

  /// Tokens for the theme currently active on [context].
  static FibrColors of(BuildContext context) =>
      Theme.of(context).brightness == Brightness.dark ? FibrColors.dark : FibrColors.light;
}

/// ThemeData built from the tokens. Inter is the intended family (weights
/// 600/800 only); if the font assets are not bundled Flutter falls back to
/// the platform font without errors — see pubspec.yaml.
class AppTheme {
  static const fontFamily = 'Inter';

  static ThemeData dark() => _build(FibrColors.dark, Brightness.dark);
  static ThemeData light() => _build(FibrColors.light, Brightness.light);

  // ---------------------------------------------------------------------
  // TV (10-foot) sizing: separate values layered ON TOP of the phone
  // themes, so the phone theme above stays untouched. Text 20→28sp
  // (×1.4), paddings ×1.5 — enough to read from a couch without breaking
  // Material density.
  // ---------------------------------------------------------------------

  /// Body text 20sp on a phone reads as 28sp on TV (feature 2 spec).
  static const tvTextScale = 1.4;

  /// Control paddings ×1.5 for remote-friendly hit areas.
  static const tvPaddingScale = 1.5;

  /// Convenience for one-off TV paddings: tvPad(16) → 24.
  static double tvPad(double v) => v * tvPaddingScale;

  static ThemeData tvDark() => _tv(FibrColors.dark, Brightness.dark);
  static ThemeData tvLight() => _tv(FibrColors.light, Brightness.light);

  /// Phone theme with TV text/padding scales re-applied (same tokens, same
  /// colors — only the sizes move).
  static ThemeData _tv(FibrColors c, Brightness brightness) {
    final base = _build(c, brightness);
    final text = base.textTheme.apply(fontSizeFactor: tvTextScale);
    return base.copyWith(
      textTheme: text,
      appBarTheme: base.appBarTheme.copyWith(titleTextStyle: text.titleLarge),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          backgroundColor: c.primary,
          foregroundColor: c.onPrimary,
          disabledBackgroundColor: c.surface2,
          disabledForegroundColor: c.muted,
          textStyle: text.labelLarge,
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
          padding: EdgeInsets.symmetric(
            horizontal: 16 * tvPaddingScale,
            vertical: 12 * tvPaddingScale,
          ),
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          foregroundColor: c.ink,
          backgroundColor: c.surface,
          side: BorderSide(color: c.hairline),
          textStyle: text.labelLarge,
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
          padding: EdgeInsets.symmetric(
            horizontal: 16 * tvPaddingScale,
            vertical: 12 * tvPaddingScale,
          ),
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(
          foregroundColor: c.secondaryStrong,
          textStyle: text.labelLarge,
        ),
      ),
      inputDecorationTheme: base.inputDecorationTheme.copyWith(
        contentPadding: EdgeInsets.symmetric(
          horizontal: 12 * tvPaddingScale,
          vertical: 10 * tvPaddingScale,
        ),
      ),
    );
  }

  static ThemeData _build(FibrColors c, Brightness brightness) {
    final scheme = ColorScheme(
      brightness: brightness,
      primary: c.primary,
      onPrimary: c.onPrimary,
      secondary: c.secondary,
      onSecondary: Colors.white,
      secondaryContainer: c.secondary,
      onSecondaryContainer: Colors.white,
      error: c.danger,
      onError: Colors.white,
      surface: c.surface,
      onSurface: c.ink,
      onSurfaceVariant: c.muted,
      outline: c.hairline,
    );

    final base = brightness == Brightness.dark
        ? Typography.material2021().black
        : Typography.material2021().white;
    final applied = base.apply(
      fontFamily: fontFamily,
      bodyColor: c.ink,
      displayColor: c.ink,
    );
    // Headings/titles are 800, everything else 600 (tokens: "no regular
    // weights in this system").
    final text = applied.copyWith(
      headlineMedium: applied.headlineMedium?.copyWith(fontWeight: FontWeight.w800),
      headlineSmall: applied.headlineSmall?.copyWith(fontWeight: FontWeight.w800),
      titleLarge: applied.titleLarge?.copyWith(fontWeight: FontWeight.w800),
      titleMedium: applied.titleMedium?.copyWith(fontWeight: FontWeight.w800),
      titleSmall: applied.titleSmall?.copyWith(fontWeight: FontWeight.w800),
    );

    OutlineInputBorder border(Color color, {double width = 1}) =>
        OutlineInputBorder(
          borderRadius: BorderRadius.circular(8),
          borderSide: BorderSide(color: color, width: width),
        );

    return ThemeData(
      useMaterial3: true,
      brightness: brightness,
      colorScheme: scheme,
      scaffoldBackgroundColor: c.canvas,
      textTheme: text,
      appBarTheme: AppBarTheme(
        backgroundColor: c.canvas,
        foregroundColor: c.ink,
        elevation: 0,
        scrolledUnderElevation: 0,
        centerTitle: false,
        titleTextStyle: text.titleLarge,
      ),
      dividerTheme: DividerThemeData(color: c.hairline, thickness: 1, space: 1),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          backgroundColor: c.primary,
          foregroundColor: c.onPrimary,
          disabledBackgroundColor: c.surface2,
          disabledForegroundColor: c.muted,
          textStyle: text.labelLarge,
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          foregroundColor: c.ink,
          backgroundColor: c.surface,
          side: BorderSide(color: c.hairline),
          textStyle: text.labelLarge,
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(
          foregroundColor: c.secondaryStrong,
          textStyle: text.labelLarge,
        ),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: c.surface,
        hintStyle: TextStyle(fontWeight: FontWeight.w600, color: c.faint),
        contentPadding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
        border: border(c.hairline),
        enabledBorder: border(c.hairline),
        focusedBorder: border(c.secondary, width: 2),
      ),
      snackBarTheme: SnackBarThemeData(
        behavior: SnackBarBehavior.floating,
        backgroundColor: c.surface,
        contentTextStyle: text.bodyMedium?.copyWith(color: c.ink),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(16),
          side: BorderSide(color: c.hairline),
        ),
      ),
      bottomSheetTheme: BottomSheetThemeData(
        backgroundColor: c.surface,
        modalBackgroundColor: c.surface,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
        ),
      ),
      checkboxTheme: CheckboxThemeData(
        fillColor: WidgetStateProperty.resolveWith(
          (states) => states.contains(WidgetState.selected) ? c.primary : null,
        ),
      ),
      radioTheme: RadioThemeData(
        fillColor: WidgetStateProperty.resolveWith(
          (states) => states.contains(WidgetState.selected) ? c.primary : c.faint,
        ),
      ),
    );
  }
}
