import { Component, ChangeDetectorRef } from '@angular/core';
import * as _ from "lodash";

// Services
import { ChartsService } from '../charts.service';

@Component({
    selector: 'last-seen-table',
    templateUrl: './last-seen-table.component.html',
    styleUrls: ['./last-seen-table.component.scss'],
    providers: [ChartsService],
})

export class LastSeenTableComponent {

    lastSeenValues: Object;

    constructor(private chartsService: ChartsService,
                private ref: ChangeDetectorRef) {

        this.lastSeenValues = {};

        let obsv = this.chartsService.getObservable();
        obsv.subscribe( (evt: any)  => {
            this.updateLastSeenTable(JSON.parse(evt.data));
        });
    }

    /**
     * Function to keep the last seen timestamps of each metric up to date
     */
    updateLastSeenTable(data: any) {
        let newValues = {};
        newValues[data.Sender + " (" + data.SourceIP + ")"] = new Date(data.Timestamp * 1000);
        Object.assign(this.lastSeenValues, newValues);
        // Lets angular know there are new changes to update the UI
        this.ref.detectChanges();
    }

    /**
     * Used by the HTML ngFor to filter through the keys of lastSeenValues
     */
    get getLastSeenKeys() {
        return _.keys(this.lastSeenValues).sort();
    }
}
