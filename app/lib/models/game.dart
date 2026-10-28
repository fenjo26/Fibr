/// Game catalog model — one entry of `GET /api/games`
/// (docs/dns-games.md §3) plus an offline snapshot used when the server
/// is unreachable.
library;

/// One game of the catalog: id + bilingual name + how many subnets its
/// routes span.
class GameInfo {
  const GameInfo({
    required this.id,
    required this.name,
    required this.nameRu,
    required this.cidrCount,
  });

  final String id;
  final String name;
  final String nameRu;
  final int cidrCount;

  /// English name by default, Russian when the UI is ru.
  String displayName(String langCode) =>
      langCode == 'ru' && nameRu.isNotEmpty ? nameRu : name;

  factory GameInfo.fromJson(Map<String, dynamic> json) => GameInfo(
        id: json['id']?.toString() ?? '',
        name: json['name']?.toString() ?? '',
        nameRu: json['name_ru']?.toString() ?? '',
        cidrCount: (json['cidr_count'] as num?)?.toInt() ?? 0,
      );
}

/// Offline snapshot of the catalog, shown when `GET /api/games` fails or
/// returns nothing. Снапшот, сервер отдаёт актуальное (обновление раз в
/// 24ч, docs/dns-games.md §2) — подсети здесь укорочены до нескольких
/// характерных блоков, чтобы экран оставался рабочим офлайн.
const kFallbackGames = <GameInfo>[
  GameInfo(id: 'cs2', name: 'CS2', nameRu: 'CS2', cidrCount: 5),
  GameInfo(id: 'dota2', name: 'Dota 2', nameRu: 'Dota 2', cidrCount: 5),
  GameInfo(id: 'valorant', name: 'Valorant', nameRu: 'Valorant', cidrCount: 4),
  GameInfo(id: 'lol', name: 'League of Legends', nameRu: 'League of Legends', cidrCount: 4),
  GameInfo(id: 'fortnite', name: 'Fortnite', nameRu: 'Fortnite', cidrCount: 3),
  GameInfo(id: 'minecraft', name: 'Minecraft', nameRu: 'Minecraft', cidrCount: 3),
];

/// Snapshot routes per game id — Valve blocks for CS2/Dota 2, Riot for
/// Valorant/LoL, AWS chunks for Fortnite, OVH for Minecraft hosting.
const _kFallbackCidrs = <String, List<String>>{
  'cs2': [
    '155.133.224.0/22', // Valve EU signaling
    '155.133.228.0/22',
    '146.66.152.0/21',
    '185.25.180.0/22',
    '103.10.124.0/23',
  ],
  'dota2': [
    '146.66.155.0/24', // Valve Dota signaling
    '146.66.156.0/22',
    '155.133.236.0/22',
    '155.133.240.0/22',
    '45.121.184.0/22', // SEA relay
  ],
  'valorant': [
    '162.249.72.0/21', // Riot AS6507
    '64.7.194.0/24',
    '103.240.192.0/22',
    '162.249.80.0/22',
  ],
  'lol': [
    '162.249.72.0/22', // Riot game servers
    '64.7.194.0/24',
    '103.240.192.0/22',
    '192.64.174.0/24', // NA chat/login
  ],
  'fortnite': [
    '18.204.0.0/14', // AWS us-east-1 chunks
    '3.208.0.0/13',
    '52.4.0.0/14',
  ],
  'minecraft': [
    '51.83.128.0/17', // OVH hosting
    '145.239.0.0/16',
    '51.68.0.0/16',
  ],
};

/// Subnets of [gameId] from the offline snapshot; empty when unknown —
/// callers then drop the game from the route union.
List<String> fallbackCidrs(String gameId) => _kFallbackCidrs[gameId] ?? const [];
