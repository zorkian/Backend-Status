#!/usr/bin/perl
#
# test-frontend.pl
#
# A test frontend for the Backend Status project.  This downloads the JSON
# object and prints out some statistics on your backends.
#
# No time was spent making this code readable/nice.  I was just hacking it
# together to test the Go server.  Sorry.
#
# by Mark Smith <mark@qq.is>
#

use LWP::Simple qw/ get /;
use JSON;  # stupid Hardy has the old module
use Data::Dumper;

my $json = new JSON;
my $obj = $json->jsonToObj(get( 'http://127.0.0.1:9464/world.json' ));

my $ct = $obj->{CurrentTime};
$obj = $obj->{World};

# print out a summary of how long requests are taking for each backend
my %sum;
foreach my $be (values %$obj) {
    my $ipp = $be->{Ipport};

    my @times;

    my $ct = $sum{$ipp}->{count} = scalar @{$be->{Completed}};
    foreach my $req (@{$be->{Completed}}) {
        $sum{$ipp}->{reqtime} += $req->{Time};
        $sum{$ipp}->{maxtime} = $req->{Time}
            if $req->{Time} > $sum{$ipp}->{maxtime};
        push @times, $req->{Time};
    }
    if ($sum{$ipp}->{reqtime} > 0) {
        $sum{$ipp}->{reqtime} /= $ct;
    }

    @times = sort @times;

    # calculate percentiles if we have at least 500 results
    if ($ct >= 500) {
        $sum{$ipp}->{p95th} = $times[int($ct * 0.95)];
        $sum{$ipp}->{p99th} = $times[int($ct * 0.99)];
    }
}

foreach my $ipp (sort keys %sum) {
    printf "%20s: mean=%0.3fs 95th=%0.3fs 99th=%0.3fs max=%0.3fs over %d requests\n",
           $ipp, $sum{$ipp}->{reqtime}, $sum{$ipp}->{p95th}, $sum{$ipp}->{p99th}, $sum{$ipp}->{maxtime}, $sum{$ipp}->{count};
}

# now print out in-flight requests, sorted by time in flight
my @if;
foreach my $be (values %$obj) {
    foreach $fe (values %{$be->{InFlight}}) {
        foreach $req (values %$fe) {
            $req->{Ipport} = $be->{Ipport};
            $req->{Elapsed} = ($ct - $req->{StartTime}) / 1000 / 1000 / 1000;
            push @if, $req;
        }
    }
}

@if = sort { $b->{Elapsed} <=> $a->{Elapsed} } @if;

print "\nIn-flight longest running:\n";
foreach my $req (@if) {
    printf "%0.3fs %20s %s\n", $req->{Elapsed}, $req->{Ipport}, $req->{Uri};
}
