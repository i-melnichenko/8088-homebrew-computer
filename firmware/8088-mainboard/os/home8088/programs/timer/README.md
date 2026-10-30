# TIMER

Native COM countdown timer for HOME8088. Build with `make os`, then `RUN TIMER`.
The default image includes CITY, TIMER and TERMINAL.

The countdown display appears only on the LCD. UART carries plain startup/help,
state/duration changes and exit messages; it does not mirror the screen or print
each second. Keyboard-only mode suppresses UART messages. The default duration is one
minute. Enter or Space starts/pauses/resumes; +/- changes the duration in
30-second steps while stopped (30 seconds to 60 minutes) and resets the countdown.
R resets to the selected duration; Esc returns to the HOME8088 shell.
At zero the timer displays DONE!; Enter starts a new countdown.

Time comes from BIOS PIT ticks, accumulating 65536 input clocks per tick and
1193182 clocks per second. Fractional time survives pause/resume. Serial output
and LCD rendering do not set the clock rate. A verified PIT is required; no
software delay is presented as accurate time. The display updates only changed
LCD cells. No speaker/alarm hardware is assumed.
