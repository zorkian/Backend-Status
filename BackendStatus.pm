#
# Perlbal::Plugin::BackendStatus
#
# Watches ClientProxy/BackendHTTP events and sends UDP packets off to a listening
# server.  That's it.
#
# by Mark Smith <mark@qq.is>
#

package Perlbal::Plugin::BackendStatus;

use strict;
use warnings;

use IO::Socket::INET;
use JSON;
use Time::HiRes qw(gettimeofday tv_interval);

my $ipport;
my $conn;
my $id = 0;

# called when we're being added to a service
sub register {
    my ($class, $svc) = @_;

    $svc->register_hook('BackendStatus', 'backend_client_assigned', sub {
        return 0 unless defined $conn;

        my Perlbal::BackendHTTP $be = $_[0];
        my Perlbal::ClientProxy $cp = $be->{client};
        $cp->{scratch}->{bs_id} = $id++;

        # construct the hashref we'll JSONify and send out
        my $pkt = encode_json { C => 1,
                                I => $cp->{scratch}->{bs_id},
                                B => $be->{ipport},
                                U => $cp->{req_headers}->request_uri };

        $conn->send($pkt);

        return 0;
    });

    $svc->register_hook('BackendStatus', 'backend_response_received', sub {
        return 0 unless defined $conn;

        my Perlbal::BackendHTTP $be = $_[0];
        my Perlbal::ClientProxy $cp = $be->{client};
        return 0 unless exists $cp->{scratch}->{bs_id};

        my $pkt = encode_json { C => 2,
                                I => $cp->{scratch}->{bs_id},
                                B => $be->{ipport},
                                T => tv_interval($cp->{last_request_time_hr}),
                                R => $be->{res_headers}->response_code };

        $conn->send($pkt);

        return 0;
    });

    return 1;
}

# called when we're no longer active on a service
sub unregister {
    my ($class, $svc) = @_;

    # clean up time
    $svc->unregister_hooks('BackendStatus');
    return 1;
}

# called when we are loaded
sub load {
    # setup a management command to dump statistics
    Perlbal::register_global_hook("manage_command.backend_status_server", sub {
        my $mc = shift->parse(qr/^backend_status_server\s+(\d+\.\d+\.\d+\.\d+)(?::(\d+))?$/i);
        my ($ip, $port) = $mc->args;

        $port ||= 9463;
        $ipport = "$ip:$port";
        $conn = IO::Socket::INET->new(PeerAddr => $ipport, Proto => 'udp')
            or die "Failed to create: $!\n";

        return $mc->ok;
    });

    return 1;
}

# called for a global unload
sub unload {
    Perlbal::unregister_global_hook('manage_command.backendstatus');
    return 1;
}

# return the config items for this one
sub dumpconfig {
    return ("BACKEND_STATUS_SERVER $ipport");
}

# escape a URL so we can send it out in a JSON object
sub eurl {
    my $a = $_[0];
    $a =~ s/([^a-zA-Z0-9_\,\-.\/\\\: ])/uc sprintf("%%%02x",ord($1))/eg;
    $a =~ tr/ /+/;
    return $a;
}

1;
