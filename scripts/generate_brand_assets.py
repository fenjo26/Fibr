#!/usr/bin/env python3
"""
Fibr Brand Identity & Asset Generator
Generates SVG and PNG assets for:
- Android App (Adaptive icons, mipmaps, Play Store)
- Web Cabinet (Favicon, Apple touch icon, PWA icons)
- Telegram Mini App (Avatar, favicon)
- Admin Panel (Favicon)
- Design system master vector files
"""

import math
import os
from PIL import Image, ImageDraw

SQRT3 = math.sqrt(3)

# Brand colors from docs/DESIGN.md & design/tokens.css
COLORS = {
    'canvas': (20, 20, 21, 255),        # #141415
    'canvas_hex': '#141415',
    'surface': (46, 48, 53, 255),       # #2e3035
    'surface_hex': '#2e3035',
    'hairline': (45, 47, 51, 255),      # #2d2f33
    'hairline_hex': '#2d2f33',
    'ink': (240, 242, 245, 255),        # #f0f2f5
    'ink_hex': '#f0f2f5',
    'muted': (154, 161, 177, 255),      # #9aa1b1
    'muted_hex': '#9aa1b1',
    'primary': (0, 178, 75, 255),       # #00b24b (Action Green)
    'primary_hex': '#00b24b',
    'primary_strong': (0, 204, 87, 255),# #00cc57
    'primary_strong_hex': '#00cc57',
    'secondary': (0, 117, 255, 255),    # #0075ff (Electric Blue)
    'secondary_hex': '#0075ff',
}

def get_geometry(size=512, scale=1.0, icon_scale=1.0):
    """Computes exact geometry coordinates for the Fibr tri-node symbol."""
    cx = size / 2.0
    cy = size / 2.0
    
    # Base proportions on 512x512
    r_node_dist = 172.0 * (size / 512.0) * icon_scale
    r_ring_center = 35.5 * (size / 512.0) * icon_scale
    stroke_ring = 14.0 * (size / 512.0) * icon_scale
    ray_len = 82.0 * (size / 512.0) * icon_scale
    stroke_ray = 14.0 * (size / 512.0) * icon_scale

    # 3 node centers: Left (180 deg), Top-Right (-60 deg), Bottom-Right (+60 deg)
    nodes = [
        (cx - r_node_dist, cy),
        (cx + r_node_dist * 0.5, cy - r_node_dist * (SQRT3 / 2.0)),
        (cx + r_node_dist * 0.5, cy + r_node_dist * (SQRT3 / 2.0))
    ]
    
    # 3 ray endpoints
    ray_ends = [
        (cx - ray_len, cy),
        (cx + ray_len * 0.5, cy - ray_len * (SQRT3 / 2.0)),
        (cx + ray_len * 0.5, cy + ray_len * (SQRT3 / 2.0))
    ]
    
    return {
        'cx': cx, 'cy': cy,
        'r_node_dist': r_node_dist,
        'r_ring_center': r_ring_center,
        'stroke_ring': stroke_ring,
        'ray_len': ray_len,
        'stroke_ray': stroke_ray,
        'nodes': nodes,
        'ray_ends': ray_ends
    }

def render_png(size=512, bg_type='squircle', theme='brand', icon_scale=1.0):
    """
    Renders high-quality anti-aliased PNG using 4x supersampling.
    bg_type: 'squircle', 'circle', 'square', 'transparent'
    theme: 'brand' (green hub accent), 'mono' (stealth grey & white), 'neon'
    """
    SS = 4  # 4x super sampling
    canvas_size = size * SS
    img = Image.new('RGBA', (canvas_size, canvas_size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    
    # Draw Background
    if bg_type == 'squircle':
        rad = int(canvas_size * 0.22)
        draw.rounded_rectangle([0, 0, canvas_size - 1, canvas_size - 1], radius=rad, fill=COLORS['canvas'])
        # Hairline subtle border
        border_w = max(1, int(2 * SS * (size / 512.0)))
        draw.rounded_rectangle([0, 0, canvas_size - 1, canvas_size - 1], radius=rad, outline=COLORS['hairline'], width=border_w)
    elif bg_type == 'circle':
        draw.ellipse([0, 0, canvas_size - 1, canvas_size - 1], fill=COLORS['canvas'])
        border_w = max(1, int(2 * SS * (size / 512.0)))
        draw.ellipse([0, 0, canvas_size - 1, canvas_size - 1], outline=COLORS['hairline'], width=border_w)
    elif bg_type == 'square':
        draw.rectangle([0, 0, canvas_size - 1, canvas_size - 1], fill=COLORS['canvas'])

    geom = get_geometry(size=canvas_size, icon_scale=icon_scale)
    cx, cy = geom['cx'], geom['cy']
    
    # Choose theme colors
    if theme == 'mono':
        ray_color = COLORS['muted']
        ring_colors = [COLORS['ink'], COLORS['ink'], COLORS['ink']]
        hub_dot_color = None
    elif theme == 'brand':
        # Green active pulse on connector hub + crisp white nodes
        ray_color = COLORS['muted']
        ring_colors = [COLORS['ink'], COLORS['ink'], COLORS['ink']]
        hub_dot_color = COLORS['primary']
    elif theme == 'brand_green':
        # Full brand green connector + white nodes
        ray_color = COLORS['primary']
        ring_colors = [COLORS['ink'], COLORS['ink'], COLORS['ink']]
        hub_dot_color = COLORS['primary_strong']
    elif theme == 'duo':
        # Primary node green (left node), top/bottom white, ray muted
        ray_color = COLORS['muted']
        ring_colors = [COLORS['primary_strong'], COLORS['ink'], COLORS['ink']]
        hub_dot_color = COLORS['primary']
    else:
        ray_color = COLORS['muted']
        ring_colors = [COLORS['ink'], COLORS['ink'], COLORS['ink']]
        hub_dot_color = None

    stroke_ray = int(geom['stroke_ray'])
    ray_r = stroke_ray / 2.0

    # Draw Central Rays
    for (rx, ry) in geom['ray_ends']:
        draw.line([(cx, cy), (rx, ry)], fill=ray_color, width=stroke_ray)
        # Round cap at ray end
        draw.ellipse([rx - ray_r, ry - ray_r, rx + ray_r, ry + ray_r], fill=ray_color)
    
    # Round join at center
    draw.ellipse([cx - ray_r, cy - ray_r, cx + ray_r, cy + ray_r], fill=ray_color)

    # Optional central hub dot
    if hub_dot_color:
        dot_r = ray_r * 0.8
        draw.ellipse([cx - dot_r, cy - dot_r, cx + dot_r, cy + dot_r], fill=hub_dot_color)

    # Draw Node Rings
    stroke_ring = int(geom['stroke_ring'])
    r_center = geom['r_ring_center']
    for idx, (nx, ny) in enumerate(geom['nodes']):
        r_outer = r_center + stroke_ring / 2.0
        r_inner = r_center - stroke_ring / 2.0
        col = ring_colors[idx]
        
        # Outer circle
        draw.ellipse([nx - r_outer, ny - r_outer, nx + r_outer, ny + r_outer], fill=col)
        # Inner punch out (hole)
        bg_hole = COLORS['canvas'] if bg_type != 'transparent' else (0, 0, 0, 0)
        draw.ellipse([nx - r_inner, ny - r_inner, nx + r_inner, ny + r_inner], fill=bg_hole)

    # Downsample with high-quality Lanczos filter
    final_img = img.resize((size, size), Image.Resampling.LANCZOS)
    return final_img

def generate_svg(bg_type='squircle', theme='brand', size=512, icon_scale=1.0):
    """Generates clean, scalable SVG vector markup."""
    geom = get_geometry(size=size, icon_scale=icon_scale)
    cx, cy = geom['cx'], geom['cy']
    
    if theme == 'mono':
        ray_col = COLORS['muted_hex']
        ring_cols = [COLORS['ink_hex'], COLORS['ink_hex'], COLORS['ink_hex']]
        hub_accent = ''
    elif theme == 'brand_green':
        ray_col = COLORS['primary_hex']
        ring_cols = [COLORS['ink_hex'], COLORS['ink_hex'], COLORS['ink_hex']]
        hub_accent = f'<circle cx="{cx:.1f}" cy="{cy:.1f}" r="{geom["stroke_ray"]*0.4:.1f}" fill="{COLORS["primary_strong_hex"]}"/>'
    elif theme == 'duo':
        ray_col = COLORS['muted_hex']
        ring_cols = [COLORS['primary_strong_hex'], COLORS['ink_hex'], COLORS['ink_hex']]
        hub_accent = f'<circle cx="{cx:.1f}" cy="{cy:.1f}" r="{geom["stroke_ray"]*0.4:.1f}" fill="{COLORS["primary_hex"]}"/>'
    else: # 'brand'
        ray_col = COLORS['muted_hex']
        ring_cols = [COLORS['ink_hex'], COLORS['ink_hex'], COLORS['ink_hex']]
        hub_accent = f'<circle cx="{cx:.1f}" cy="{cy:.1f}" r="{geom["stroke_ray"]*0.5:.1f}" fill="{COLORS["primary_hex"]}"/>'

    svg_parts = [
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {size} {size}" width="{size}" height="{size}">'
    ]
    
    if bg_type == 'squircle':
        rad = size * 0.22
        svg_parts.append(
            f'  <rect width="{size}" height="{size}" rx="{rad:.1f}" fill="{COLORS["canvas_hex"]}" stroke="{COLORS["hairline_hex"]}" stroke-width="{max(1, size*0.004):.1f}"/>'
        )
    elif bg_type == 'circle':
        svg_parts.append(
            f'  <circle cx="{cx}" cy="{cy}" r="{size/2 - 1}" fill="{COLORS["canvas_hex"]}" stroke="{COLORS["hairline_hex"]}" stroke-width="{max(1, size*0.004):.1f}"/>'
        )

    # Connector rays
    stroke_ray = geom['stroke_ray']
    p1, p2, p3 = geom['ray_ends']
    svg_parts.append(f'  <g stroke="{ray_col}" stroke-width="{stroke_ray:.1f}" stroke-linecap="round" stroke-linejoin="round">')
    svg_parts.append(f'    <line x1="{cx:.1f}" y1="{cy:.1f}" x2="{p1[0]:.1f}" y2="{p1[1]:.1f}"/>')
    svg_parts.append(f'    <line x1="{cx:.1f}" y1="{cy:.1f}" x2="{p2[0]:.1f}" y2="{p2[1]:.1f}"/>')
    svg_parts.append(f'    <line x1="{cx:.1f}" y1="{cy:.1f}" x2="{p3[0]:.1f}" y2="{p3[1]:.1f}"/>')
    svg_parts.append('  </g>')

    if hub_accent:
        svg_parts.append(f'  {hub_accent}')

    # Outer rings
    r_center = geom['r_ring_center']
    stroke_ring = geom['stroke_ring']
    for idx, (nx, ny) in enumerate(geom['nodes']):
        col = ring_cols[idx]
        svg_parts.append(
            f'  <circle cx="{nx:.1f}" cy="{ny:.1f}" r="{r_center:.1f}" fill="none" stroke="{col}" stroke-width="{stroke_ring:.1f}"/>'
        )

    svg_parts.append('</svg>')
    return '\n'.join(svg_parts)

def generate_logo_horizontal_svg(theme='brand', width=600, height=160):
    """Generates horizontal logo with mark + 'Fibr' wordmark."""
    icon_size = 120
    icon_svg_content = generate_svg(bg_type='squircle', theme=theme, size=icon_size)
    # Extract inner content of icon svg
    lines = icon_svg_content.split('\n')[1:-1]
    inner_icon = '\n      '.join(lines)
    
    text_color = COLORS['ink_hex']
    vpn_label_color = COLORS['primary_hex']
    
    return f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {width} {height}" width="{width}" height="{height}">
  <!-- Icon Mark -->
  <g transform="translate(20, 20)">
      {inner_icon}
  </g>
  <!-- Wordmark -->
  <text x="170" y="96" font-family="Inter, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif" font-weight="800" font-size="76" fill="{text_color}" letter-spacing="-1.5">Fibr</text>
  <text x="172" y="126" font-family="Inter, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif" font-weight="600" font-size="16" fill="{vpn_label_color}" letter-spacing="4">FAST • PRIVATE • MESH</text>
</svg>'''

def generate_android_vector_foreground():
    """Generates Android vector drawable (108dp viewport, centered 72dp safe zone)."""
    # Safe zone for adaptive icon is 66-72dp in center
    geom = get_geometry(size=108, icon_scale=0.62)
    cx, cy = geom['cx'], geom['cy']
    stroke_ray = geom['stroke_ray']
    p1, p2, p3 = geom['ray_ends']
    stroke_ring = geom['stroke_ring']
    r_center = geom['r_ring_center']
    
    return f'''<vector xmlns:android="http://schemas.android.com/apk/res/android"
    android:width="108dp"
    android:height="108dp"
    android:viewportWidth="108"
    android:viewportHeight="108">
    
    <!-- Central Connector Rays -->
    <path
        android:strokeColor="#9aa1b1"
        android:strokeWidth="{stroke_ray:.2f}"
        android:strokeLineCap="round"
        android:strokeLineJoin="round"
        android:pathData="M{cx:.2f},{cy:.2f} L{p1[0]:.2f},{p1[1]:.2f} M{cx:.2f},{cy:.2f} L{p2[0]:.2f},{p2[1]:.2f} M{cx:.2f},{cy:.2f} L{p3[0]:.2f},{p3[1]:.2f}" />

    <!-- Brand Green Center Hub Pulse -->
    <path
        android:fillColor="#00b24b"
        android:pathData="M{cx:.2f},{cy - stroke_ray*0.5:.2f} A{stroke_ray*0.5:.2f},{stroke_ray*0.5:.2f} 0 1,0 {cx:.2f},{cy + stroke_ray*0.5:.2f} A{stroke_ray*0.5:.2f},{stroke_ray*0.5:.2f} 0 1,0 {cx:.2f},{cy - stroke_ray*0.5:.2f} Z" />

    <!-- Node Ring 1 (Left) -->
    <path
        android:strokeColor="#f0f2f5"
        android:strokeWidth="{stroke_ring:.2f}"
        android:pathData="M{geom['nodes'][0][0]:.2f},{geom['nodes'][0][1] - r_center:.2f} A{r_center:.2f},{r_center:.2f} 0 1,0 {geom['nodes'][0][0]:.2f},{geom['nodes'][0][1] + r_center:.2f} A{r_center:.2f},{r_center:.2f} 0 1,0 {geom['nodes'][0][0]:.2f},{geom['nodes'][0][1] - r_center:.2f} Z" />

    <!-- Node Ring 2 (Top-Right) -->
    <path
        android:strokeColor="#f0f2f5"
        android:strokeWidth="{stroke_ring:.2f}"
        android:pathData="M{geom['nodes'][1][0]:.2f},{geom['nodes'][1][1] - r_center:.2f} A{r_center:.2f},{r_center:.2f} 0 1,0 {geom['nodes'][1][0]:.2f},{geom['nodes'][1][1] + r_center:.2f} A{r_center:.2f},{r_center:.2f} 0 1,0 {geom['nodes'][1][0]:.2f},{geom['nodes'][1][1] - r_center:.2f} Z" />

    <!-- Node Ring 3 (Bottom-Right) -->
    <path
        android:strokeColor="#f0f2f5"
        android:strokeWidth="{stroke_ring:.2f}"
        android:pathData="M{geom['nodes'][2][0]:.2f},{geom['nodes'][2][1] - r_center:.2f} A{r_center:.2f},{r_center:.2f} 0 1,0 {geom['nodes'][2][0]:.2f},{geom['nodes'][2][1] + r_center:.2f} A{r_center:.2f},{r_center:.2f} 0 1,0 {geom['nodes'][2][0]:.2f},{geom['nodes'][2][1] - r_center:.2f} Z" />
</vector>'''

def main():
    base_dir = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    brand_dir = os.path.join(base_dir, 'design', 'brand')
    os.makedirs(brand_dir, exist_ok=True)
    
    print('1. Generating Master SVGs in design/brand/ ...')
    # Master SVGs
    with open(os.path.join(brand_dir, 'fibr-icon.svg'), 'w') as f:
        f.write(generate_svg(bg_type='squircle', theme='brand', size=512))
    with open(os.path.join(brand_dir, 'fibr-icon-mono.svg'), 'w') as f:
        f.write(generate_svg(bg_type='squircle', theme='mono', size=512))
    with open(os.path.join(brand_dir, 'fibr-icon-green.svg'), 'w') as f:
        f.write(generate_svg(bg_type='squircle', theme='brand_green', size=512))
    with open(os.path.join(brand_dir, 'fibr-mark.svg'), 'w') as f:
        f.write(generate_svg(bg_type='transparent', theme='brand', size=512))
    with open(os.path.join(brand_dir, 'fibr-mark-mono.svg'), 'w') as f:
        f.write(generate_svg(bg_type='transparent', theme='mono', size=512))
    with open(os.path.join(brand_dir, 'fibr-logo.svg'), 'w') as f:
        f.write(generate_logo_horizontal_svg(theme='brand'))
    with open(os.path.join(brand_dir, 'fibr-logo-mono.svg'), 'w') as f:
        f.write(generate_logo_horizontal_svg(theme='mono'))
    with open(os.path.join(brand_dir, 'favicon.svg'), 'w') as f:
        f.write(generate_svg(bg_type='squircle', theme='brand', size=64))

    print('2. Generating Master PNGs ...')
    app_icon_512 = render_png(size=512, bg_type='squircle', theme='brand')
    app_icon_512.save(os.path.join(brand_dir, 'fibr-icon-512.png'))
    app_icon_mono_512 = render_png(size=512, bg_type='squircle', theme='mono')
    app_icon_mono_512.save(os.path.join(brand_dir, 'fibr-icon-mono-512.png'))
    app_icon_1024 = render_png(size=1024, bg_type='squircle', theme='brand')
    app_icon_1024.save(os.path.join(brand_dir, 'fibr-icon-1024.png'))

    # Telegram Bot Avatar / Mini App Avatar (circular)
    avatar_512 = render_png(size=512, bg_type='circle', theme='brand')
    avatar_512.save(os.path.join(brand_dir, 'fibr-avatar-circle-512.png'))
    
    print('3. Generating Android App Icons ...')
    android_res = os.path.join(base_dir, 'app', 'android', 'app', 'src', 'main', 'res')
    
    # Adaptive Icon XMLs
    anydpi_dir = os.path.join(android_res, 'mipmap-anydpi-v26')
    os.makedirs(anydpi_dir, exist_ok=True)
    with open(os.path.join(anydpi_dir, 'ic_launcher.xml'), 'w') as f:
        f.write('''<?xml version="1.0" encoding="utf-8"?>
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@drawable/ic_launcher_background" />
    <foreground android:drawable="@drawable/ic_launcher_foreground" />
</adaptive-icon>
''')
    with open(os.path.join(anydpi_dir, 'ic_launcher_round.xml'), 'w') as f:
        f.write('''<?xml version="1.0" encoding="utf-8"?>
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@drawable/ic_launcher_background" />
    <foreground android:drawable="@drawable/ic_launcher_foreground" />
</adaptive-icon>
''')
    
    # Background and Foreground drawables
    drawable_dir = os.path.join(android_res, 'drawable')
    os.makedirs(drawable_dir, exist_ok=True)
    with open(os.path.join(drawable_dir, 'ic_launcher_background.xml'), 'w') as f:
        f.write('''<?xml version="1.0" encoding="utf-8"?>
<vector xmlns:android="http://schemas.android.com/apk/res/android"
    android:width="108dp"
    android:height="108dp"
    android:viewportWidth="108"
    android:viewportHeight="108">
    <path
        android:fillColor="#141415"
        android:pathData="M0,0h108v108h-108z"/>
</vector>
''')
    with open(os.path.join(drawable_dir, 'ic_launcher_foreground.xml'), 'w') as f:
        f.write(generate_android_vector_foreground())

    # Android Mipmaps
    densities = {
        'mipmap-mdpi': 48,
        'mipmap-hdpi': 72,
        'mipmap-xhdpi': 96,
        'mipmap-xxhdpi': 144,
        'mipmap-xxxhdpi': 192,
    }
    for folder, px in densities.items():
        target_dir = os.path.join(android_res, folder)
        os.makedirs(target_dir, exist_ok=True)
        # Standard launcher squircle
        icon_img = render_png(size=px, bg_type='squircle', theme='brand')
        icon_img.save(os.path.join(target_dir, 'ic_launcher.png'))
        # Round launcher
        round_img = render_png(size=px, bg_type='circle', theme='brand')
        round_img.save(os.path.join(target_dir, 'ic_launcher_round.png'))

    # Google Play Store 512x512
    play_icon = render_png(size=512, bg_type='squircle', theme='brand')
    play_icon.save(os.path.join(base_dir, 'app', 'android', 'playstore-icon-512.png'))

    print('4. Generating Web & Cabinet Assets ...')
    web_dirs = [
        os.path.join(base_dir, 'cabinet'),
        os.path.join(base_dir, 'miniapp'),
        os.path.join(base_dir, 'admin'),
        os.path.join(base_dir, 'design')
    ]
    for wdir in web_dirs:
        # Favicon SVG
        with open(os.path.join(wdir, 'favicon.svg'), 'w') as f:
            f.write(generate_svg(bg_type='squircle', theme='brand', size=64))
        # Apple touch icon (180x180)
        touch_img = render_png(size=180, bg_type='squircle', theme='brand')
        touch_img.save(os.path.join(wdir, 'apple-touch-icon.png'))
        # Favicon ICO (multi-size 16, 32, 48)
        icon_16 = render_png(size=16, bg_type='squircle', theme='brand')
        icon_32 = render_png(size=32, bg_type='squircle', theme='brand')
        icon_48 = render_png(size=48, bg_type='squircle', theme='brand')
        icon_32.save(
            os.path.join(wdir, 'favicon.ico'),
            format='ICO',
            sizes=[(16, 16), (32, 32), (48, 48)]
        )
        # PWA icons
        icon_192 = render_png(size=192, bg_type='squircle', theme='brand')
        icon_192.save(os.path.join(wdir, 'icon-192.png'))
        icon_512 = render_png(size=512, bg_type='squircle', theme='brand')
        icon_512.save(os.path.join(wdir, 'icon-512.png'))

    # Mini App avatar specifically
    miniapp_avatar = render_png(size=512, bg_type='circle', theme='brand')
    miniapp_avatar.save(os.path.join(base_dir, 'miniapp', 'bot-avatar.png'))

    print('✓ All assets generated successfully!')

if __name__ == '__main__':
    main()
