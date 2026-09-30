# Extra R8 rules for the release build. The libraries (Compose, Media3, Ktor,
# OkHttp, Coil, kotlinx.serialization) ship their own; this file only holds
# what they miss.

# The widget library (Glance) brings WorkManager 2.7 and Room 2.2, whose rules
# keep these classes but not the constructors they're created through by
# reflection. R8's full mode then strips those, and the app crashes at startup.
-keep class * extends androidx.room.RoomDatabase { <init>(); }
-keep class * extends androidx.work.InputMerger { <init>(); }
