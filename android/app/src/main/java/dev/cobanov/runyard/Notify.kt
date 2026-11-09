package dev.cobanov.runyard

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.net.Uri
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch

// Notifications with no Google services: Android wakes the app about every 15 minutes (the shortest period a
// JobScheduler job may have, and it may come later while the phone dozes) to ask the server what changed.
// The server's own channels (ntfy and the others) are the instant way.

const val NOTIFY_JOB = 1

/** Turns the periodic check on or off; `keep` leaves a job that is already waiting alone, so its period is not restarted. */
fun scheduleNotify(context: Context, on: Boolean, keep: Boolean = false) {
    val jobs = context.getSystemService(JobScheduler::class.java)
    if (!on) return jobs.cancel(NOTIFY_JOB)
    if (keep && jobs.getPendingJob(NOTIFY_JOB) != null) return
    jobs.schedule(
        JobInfo.Builder(NOTIFY_JOB, ComponentName(context, NotifyJob::class.java))
            .setPeriodic(15 * 60_000L).setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY).setPersisted(true).build(),
    )
}

class NotifyJob : JobService() {
    private val scope = MainScope()

    override fun onStartJob(params: JobParameters): Boolean {
        scope.launch {
            runCatching { Store.get(this@NotifyJob).check() }.getOrNull()?.forEach(::post) // a failed check waits for the next one
            jobFinished(params, false)
        }
        return true
    }

    override fun onStopJob(params: JobParameters) = false

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private fun post(n: Note) {
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(NotificationChannel("runs", "Runs and alerts", NotificationManager.IMPORTANCE_DEFAULT))
        val open = PendingIntent.getActivity(
            this, n.id, Intent(Intent.ACTION_VIEW, Uri.parse("runyard://run/${n.run}"), this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        manager.notify(n.id, Notification.Builder(this, "runs").setSmallIcon(R.drawable.ic_launcher_monochrome)
            .setContentTitle(n.title).setContentText(n.text).setContentIntent(open).setAutoCancel(true).build())
    }
}
